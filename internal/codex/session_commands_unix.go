//go:build unix

package codex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type commandModel struct {
	ID, Model, DisplayName, Description, DefaultReasoningEffort string
	Hidden                                                      bool
	SupportedReasoningEfforts                                   []struct{ ReasoningEffort, Description string }
	ServiceTiers                                                []struct{ ID, Name, Description string }
}

type commandThread struct {
	ID, Cwd, Name string
	Status        struct{ Type string }
	Environments  []struct {
		EnvironmentID string `json:"environmentId"`
	}
}

func commandDigest(value any) string {
	data, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func commandText(value string) string  { return SanitizeSessionContext(value) }
func commandSafe(value string) bool    { return value != "" && commandText(value) == value }
func commandLabel(value string) string { return strings.Join(strings.Fields(commandText(value)), " ") }

func (p *daemonStatusProvider) commandThread(ctx context.Context, id string) (commandThread, error) {
	if id == "" || len(id) > 512 {
		return commandThread{}, ErrSessionCommand
	}
	if err := p.ensureConnected(ctx); err != nil {
		return commandThread{}, err
	}
	loaded, err := p.loadedThreads(ctx)
	if err != nil {
		return commandThread{}, err
	}
	if _, ok := loaded[id]; !ok {
		return commandThread{}, ErrSessionCommand
	}
	var response struct{ Thread commandThread }
	err = p.request(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &response)
	if err != nil {
		return response.Thread, err
	}
	if response.Thread.ID != id || response.Thread.Cwd == "" {
		return commandThread{}, ErrSessionCommand
	}
	return response.Thread, nil
}

// Pagination and total inventory are bounded. Repeated cursors fail rather
// than returning a partial set which might look authoritative.
func (p *daemonStatusProvider) commandRows(ctx context.Context, method string, params map[string]any) ([]json.RawMessage, error) {
	var rows []json.RawMessage
	seen := map[string]bool{}
	for page := 0; page < 32; page++ {
		var response struct {
			Data       []json.RawMessage
			NextCursor *string
		}
		if err := p.request(ctx, method, params, &response); err != nil {
			return nil, err
		}
		rows = append(rows, response.Data...)
		if len(rows) > 2048 {
			return nil, ErrSessionCommand
		}
		if response.NextCursor == nil || *response.NextCursor == "" {
			return rows, nil
		}
		if seen[*response.NextCursor] {
			return nil, ErrSessionCommand
		}
		seen[*response.NextCursor] = true
		params["cursor"] = *response.NextCursor
	}
	return nil, ErrSessionCommand
}

func (p *daemonStatusProvider) commandModels(ctx context.Context) ([]commandModel, error) {
	rows, err := p.commandRows(ctx, "model/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var models []commandModel
	for _, raw := range rows {
		var model commandModel
		if json.Unmarshal(raw, &model) == nil && !model.Hidden && commandSafe(model.Model) {
			models = append(models, model)
		}
	}
	return models, nil
}

func (p *daemonStatusProvider) SessionCommands(ctx context.Context, id, path string) (SessionCommandMenu, error) {
	if len(path) > 2048 {
		return SessionCommandMenu{}, ErrSessionCommand
	}
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	if name, ok := strings.CutPrefix(path, "rename "); ok {
		path = "rename/" + url.PathEscape(strings.TrimSpace(name))
	}
	if directory, ok := strings.CutPrefix(path, "cd "); ok {
		path = "cd/" + url.PathEscape(strings.TrimSpace(directory))
	}
	if len(path) > 2048 {
		return SessionCommandMenu{}, ErrSessionCommand
	}
	thread, err := p.commandThread(ctx, id)
	if err != nil {
		return SessionCommandMenu{}, err
	}
	menu, err := p.commandMenu(ctx, thread, path)
	if err != nil {
		return menu, err
	}
	menu.Path = path
	p.mu.Lock()
	connection := fmt.Sprintf("%p", p.connection)
	p.mu.Unlock()
	menu.Revision = commandDigest([]any{connection, thread.ID, thread.Cwd, thread.Status.Type, thread.Name, menu.Revision})
	return normalizeCommandMenu(menu), nil
}

func normalizeCommandMenu(menu SessionCommandMenu) SessionCommandMenu {
	safeHelp := commandText(menu.Help) == strings.TrimSpace(menu.Help)
	menu.Title, menu.Help = commandLabel(menu.Title), commandText(menu.Help)
	unique := make([]SessionCommandChoice, 0, len(menu.Choices))
	seen := map[string]bool{}
	for i := range menu.Choices {
		o := &menu.Choices[i]
		if o.method != "" && (!safeHelp || commandLabel(o.Label) != o.Label || commandText(o.Help) != strings.TrimSpace(o.Help)) {
			o.method = ""
			o.params = nil
			o.Help = "Complete safe help unavailable. Use Codex for this option."
		}
		o.Label, o.Help = commandLabel(o.Label), commandText(o.Help)
		o.ID = commandDigest([]any{o.Label, o.Help, o.Next, o.method, o.params})
		o.Action = o.method != ""
		if !seen[o.ID] {
			unique = append(unique, *o)
			seen[o.ID] = true
		}
	}
	menu.Choices = unique
	return menu
}

func (p *daemonStatusProvider) commandMenu(ctx context.Context, t commandThread, path string) (SessionCommandMenu, error) {
	m := SessionCommandMenu{Title: "/" + path, Help: "Live options from the shared Codex app-server.", Choices: []SessionCommandChoice{}}
	parts := strings.Split(path, "/")
	// Directory-scoped inventories must follow running settings after /cd,
	// even while the captured thread metadata still contains the old cwd.
	if parts[0] == "permissions" || parts[0] == "skills" || parts[0] == "hooks" {
		p.mu.Lock()
		conn := p.connection
		p.mu.Unlock()
		cwd, err := p.commandDirectory(ctx, conn, t.ID)
		if err != nil {
			return m, err
		}
		t.Cwd = cwd
		m.Revision = commandDigest(cwd)
	}

	if path == "" || path == "help" {
		families := []string{"rename", "cd", "cwd", "pwd", "model", "plan", "permissions", "skills", "apps", "mcp", "hooks", "experimental"}
		menus := make([]SessionCommandMenu, len(families))
		errs := make([]error, len(families))
		var wg sync.WaitGroup
		for i, family := range families {
			wg.Add(1)
			go func() { defer wg.Done(); menus[i], errs[i] = p.commandMenu(ctx, t, family) }()
		}
		wg.Wait()
		for i, family := range families {
			if errs[i] == nil && (len(menus[i].Choices) > 0 || menus[i].Input) {
				m.Choices = append(m.Choices, SessionCommandChoice{Label: "/" + family, Help: menus[i].Help, Next: family})
			}
		}
		if models, err := p.commandModels(ctx); err == nil {
			if current, err := p.readQuotaSession(ctx, t.ID); err == nil {
				for _, model := range models {
					if model.Model == current.Model {
						for _, tier := range model.ServiceTiers {
							name := strings.ToLower(tier.Name)
							if commandSafe(tier.ID) && commandSafe(name) && !strings.ContainsAny(name, " /\t\n") && !commandReserved(name) {
								m.Choices = append(m.Choices, SessionCommandChoice{Label: "/" + name, Help: tier.Description, Next: "tier/" + url.PathEscape(tier.ID)})
							}
						}
					}
				}
			}
		}
		m.Title = "/ COMMANDS"
		m.Help = "Choose a command. Options/help are discovered live. Changes target this session only and require confirmation. Model and permission settings are available only while idle. Catalogues marked browse-only do not execute tools or change global configuration."
		return m, nil
	}
	if parts[0] == "cd" || parts[0] == "cwd" || parts[0] == "pwd" {
		return p.commandDirectoryMenu(ctx, t, path)
	}
	if parts[0] == "rename" {
		m.Title = "/rename"
		m.Help = "Rename this session in Codex. Enter a single-line name, then review and confirm the change. It does not send a prompt or change model settings."
		if path == "rename" {
			m.Input = true
			m.InputLabel, m.InputLimit = "Session name", 512
			m.Value = commandLabel(t.Name)
			return m, nil
		}
		if len(parts) != 2 {
			return m, ErrSessionCommand
		}
		name, err := url.PathUnescape(parts[1])
		if err != nil || name == "" || len([]rune(name)) > 512 || commandLabel(name) != name {
			return m, ErrSessionCommand
		}
		m.Choices = append(m.Choices, SessionCommandChoice{
			Label:  "Rename to " + name,
			Help:   "Current name: " + commandLabel(t.Name) + "\nNew name: " + name + "\nOnly this session's saved name changes.",
			method: "thread/name/set", params: map[string]any{"threadId": t.ID, "name": name},
		})
		return m, nil
	}
	if parts[0] == "model" || parts[0] == "tier" || parts[0] == "plan" || parts[0] == "permissions" {
		current, err := p.readQuotaSession(ctx, t.ID)
		if err != nil {
			return m, err
		}
		m.Revision = commandDigest([]any{current, t.Cwd})
		setting := func(label, help string, params map[string]any) {
			params["threadId"] = t.ID
			if t.Status.Type != "idle" {
				help += " — available when the session is idle"
				m.Choices = append(m.Choices, SessionCommandChoice{Label: label, Help: help})
				return
			}
			m.Choices = append(m.Choices, SessionCommandChoice{Label: label, Help: help, method: "thread/settings/update", params: params})
		}
		switch parts[0] {
		case "model", "tier":
			models, err := p.commandModels(ctx)
			if err != nil {
				return m, err
			}
			if path == "model" {
				m.Help = "Select a model, then one of its advertised reasoning levels. Changes apply to subsequent turns in this session; automatic quota thresholds may later replace them."
				for _, model := range models {
					m.Choices = append(m.Choices, SessionCommandChoice{Label: model.Model, Help: model.Description, Next: "model/" + url.PathEscape(model.Model)})
				}
				return m, nil
			}
			if len(parts) != 2 {
				return m, ErrSessionCommand
			}
			target, err := url.PathUnescape(parts[1])
			if err != nil {
				return m, err
			}
			for _, model := range models {
				if parts[0] == "model" && model.Model == target {
					m.Help = model.Description + "\nSelect reasoning effort. Existing speed will be reset to the server default to avoid carrying an unsupported tier to another model."
					for _, effort := range model.SupportedReasoningEfforts {
						if commandSafe(effort.ReasoningEffort) {
							setting(effort.ReasoningEffort, effort.Description, map[string]any{"model": model.Model, "effort": effort.ReasoningEffort, "serviceTier": nil})
						}
					}
					if len(model.SupportedReasoningEfforts) == 0 && commandSafe(model.DefaultReasoningEffort) {
						setting(model.DefaultReasoningEffort, "Server-advertised default reasoning effort.", map[string]any{"model": model.Model, "effort": model.DefaultReasoningEffort, "serviceTier": nil})
					}
					return m, nil
				}
				if parts[0] == "tier" && model.Model == current.Model {
					for _, tier := range model.ServiceTiers {
						if tier.ID == target {
							m.Title = "/" + strings.ToLower(tier.Name)
							m.Help = tier.Description
							setting("Enable "+tier.Name, tier.Description, map[string]any{"serviceTier": tier.ID})
							setting("Use default tier", "Clear this session's explicit speed override.", map[string]any{"serviceTier": nil})
							return m, nil
						}
					}
				}
			}
		case "permissions":
			m.Help = "Permission profiles allowed by the effective requirements for this directory. Review the scope carefully: this changes what Codex may do without asking in subsequent turns."
			rows, err := p.commandRows(ctx, "permissionProfile/list", map[string]any{"cwd": t.Cwd})
			if err != nil {
				return m, err
			}
			for _, raw := range rows {
				var v struct {
					ID, Description string
					Allowed         bool
				}
				if json.Unmarshal(raw, &v) == nil && v.Allowed && commandSafe(v.ID) && strings.TrimSpace(v.Description) != "" {
					setting(v.ID, v.Description, map[string]any{"permissions": v.ID})
				}
			}
			return m, nil
		case "plan":
			m.Help = "Choose an advertised collaboration mode for subsequent turns. Uses Codex's built-in mode instructions, not a generated prompt."
			rows, err := p.commandRows(ctx, "collaborationMode/list", map[string]any{})
			if err != nil {
				return m, err
			}
			for _, raw := range rows {
				var v struct {
					Name, Mode string
					Model      *string
					Effort     *string `json:"reasoning_effort"`
				}
				if json.Unmarshal(raw, &v) != nil || !commandSafe(v.Mode) {
					continue
				}
				model, effort := current.Model, current.Effort
				if v.Model != nil {
					model = *v.Model
				}
				if v.Effort != nil {
					effort = *v.Effort
				}
				setting(v.Name, v.Mode+" // "+model+" // "+effort, map[string]any{"collaborationMode": map[string]any{"mode": v.Mode, "settings": map[string]any{"model": model, "reasoning_effort": effort, "developer_instructions": nil}}})
			}
			return m, nil
		}
		return m, ErrSessionCommand
	}
	if len(parts) == 1 {
		return p.commandCatalogue(ctx, t, path)
	}
	return m, ErrSessionCommand
}

func commandReserved(s string) bool {
	for _, name := range []string{"help", "rename", "cd", "cwd", "pwd", "model", "tier", "plan", "permissions", "skills", "apps", "mcp", "hooks", "experimental"} {
		if s == name {
			return true
		}
	}
	return false
}

func (p *daemonStatusProvider) ExecuteSessionCommand(ctx context.Context, id, path, revision, choice string) error {
	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	p.mu.Lock()
	conn := p.connection
	p.mu.Unlock()
	if conn == nil || p.settingsClosed {
		return ErrSessionCommand
	}
	m, err := p.SessionCommands(ctx, id, path)
	if err != nil {
		return err
	}
	if m.Revision != revision {
		return ErrSessionCommand
	}
	for _, o := range m.Choices {
		if o.ID == choice && o.Action {
			p.mu.Lock()
			valid := p.connection == conn && (o.method == "thread/name/set" || p.contexts[id] == nil || len(p.contexts[id].requests) == 0)
			p.mu.Unlock()
			if !valid {
				return ErrSessionCommand
			}
			if cwd, changingDirectory := o.params["cwd"].(string); changingDirectory {
				var queue struct {
					Data       *[]json.RawMessage
					NextCursor *string
				}
				if err := p.requestOn(ctx, conn, "thread/queue/list", map[string]any{"threadId": id, "limit": 1}, &queue); err != nil {
					return err
				}
				if queue.Data == nil || len(*queue.Data) != 0 || (queue.NextCursor != nil && *queue.NextCursor != "") {
					return ErrSessionCommand
				}
				if err := p.requestOn(ctx, conn, o.method, o.params, nil); err != nil {
					return err
				}
				// An empty settings response acknowledges queuing only. Read the
				// running configuration before reporting success; old servers may
				// silently ignore a field they do not recognise. Never retry writes.
				verifyCtx, cancel := context.WithTimeout(ctx, daemonStatusRefreshEvery)
				defer cancel()
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()
				for {
					active, err := p.commandDirectory(verifyCtx, conn, id)
					if err != nil {
						return err
					}
					if active == cwd {
						return nil
					}
					select {
					case <-verifyCtx.Done():
						return fmt.Errorf("directory update unconfirmed: %w", verifyCtx.Err())
					case <-ticker.C:
					}
				}
			}
			return p.requestOn(ctx, conn, o.method, o.params, nil)
		}
	}
	return ErrSessionCommand
}

// Read-only inventory deliberately does not infer toggle, installation, OAuth
// or arbitrary tool-execution semantics from a discovered name.
func (p *daemonStatusProvider) commandCatalogue(ctx context.Context, t commandThread, path string) (SessionCommandMenu, error) {
	m := SessionCommandMenu{Title: "/" + path, Help: "Browse-only catalogue from Codex. Select an entry to read its supplied help; use Codex to invoke or configure it.", Choices: []SessionCommandChoice{}}
	method := map[string]string{"skills": "skills/list", "apps": "app/list", "mcp": "mcpServerStatus/list", "hooks": "hooks/list", "experimental": "experimentalFeature/list"}[path]
	if method == "" { // Catalog tier aliases such as /fast, never assumed IDs.
		root, err := p.commandMenu(ctx, t, "")
		if err != nil {
			return m, err
		}
		for _, o := range root.Choices {
			if o.Label == "/"+path {
				return p.commandMenu(ctx, t, o.Next)
			}
		}
		return m, ErrSessionCommand
	}
	params := map[string]any{}
	switch path {
	case "skills", "hooks":
		params["cwds"] = []string{t.Cwd}
	case "apps", "mcp":
		params["threadId"] = t.ID
	}
	rows, err := p.commandRows(ctx, method, params)
	if err != nil {
		return m, err
	}
	overflow := false
	add := func(label, help string) {
		if len(m.Choices) < 2048 {
			m.Choices = append(m.Choices, SessionCommandChoice{Label: label, Help: help})
		} else {
			overflow = true
		}
	}
	for _, raw := range rows {
		switch path {
		case "skills":
			var entry struct {
				Cwd    string
				Skills []struct {
					Name, Description, Path string
					Enabled                 bool
				}
			}
			if json.Unmarshal(raw, &entry) == nil && entry.Cwd == t.Cwd {
				for _, s := range entry.Skills {
					if s.Enabled {
						add(s.Name, s.Description+"\n"+s.Path)
					}
				}
			}
		case "hooks":
			var entry struct {
				Cwd   string
				Hooks []struct {
					Key, EventName, StatusMessage, Command, Server, Tool, HandlerType string
					Enabled                                                           bool
				}
			}
			if json.Unmarshal(raw, &entry) == nil && entry.Cwd == t.Cwd {
				for _, h := range entry.Hooks {
					add(h.Key, fmt.Sprintf("%s // %s // enabled=%t\n%s\n%s %s %s", h.EventName, h.HandlerType, h.Enabled, h.StatusMessage, h.Command, h.Server, h.Tool))
				}
			}
		case "apps":
			var a struct {
				Name, Description       string
				IsAccessible, IsEnabled bool
			}
			if json.Unmarshal(raw, &a) == nil && a.IsAccessible && a.IsEnabled {
				add(a.Name, a.Description)
			}
		case "experimental":
			var f struct {
				Name, DisplayName, Description, Announcement, Stage string
				Enabled                                             bool
			}
			if json.Unmarshal(raw, &f) == nil {
				add(f.Name, fmt.Sprintf("%s // %s // enabled=%t\n%s\n%s", f.DisplayName, f.Stage, f.Enabled, f.Description, f.Announcement))
			}
		case "mcp":
			var s struct {
				Name, AuthStatus, ToolsError string
				Tools                        map[string]struct {
					Name        string
					Description string
				}
			}
			if json.Unmarshal(raw, &s) == nil {
				add(s.Name, "Authentication: "+s.AuthStatus+"\n"+s.ToolsError)
				names := make([]string, 0, len(s.Tools))
				for name := range s.Tools {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					add(s.Name+" / "+name, s.Tools[name].Description)
				}
			}
		}
	}
	if overflow {
		return SessionCommandMenu{}, ErrSessionCommand
	}
	return m, nil
}
