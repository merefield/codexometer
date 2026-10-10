//go:build unix

package codex

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func directoryFixture(t *testing.T) (*daemonStatusProvider, *quotaDaemonFixture, string) {
	t.Helper()
	p, f := newQuotaDaemon(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f.cwds = map[string]string{"one": base, "two": "/other"}
	return p, f, base
}

func TestSessionDirectoryReadAndScopedChange(t *testing.T) {
	p, f, base := directoryFixture(t)
	ctx := context.Background()
	target := filepath.Join(base, "two  spaces 界")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"cwd", "pwd"} {
		m, err := p.SessionCommands(ctx, "one", alias)
		if err != nil || len(m.Choices) != 1 || m.Choices[0].Action || !strings.Contains(m.Choices[0].Help, base) {
			t.Fatal(m, err)
		}
		if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
			t.Fatal("read-only directory executed")
		}
	}
	root, err := p.SessionCommands(ctx, "one", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"cd", "cwd", "pwd"} {
		found := false
		for _, o := range root.Choices {
			found = found || o.Next == command
		}
		if !found || !commandReserved(command) {
			t.Fatal("missing directory command", command)
		}
	}
	editor, err := p.SessionCommands(ctx, "one", "cd")
	if err != nil || !editor.Input || editor.Value != base || editor.InputLabel != "Working directory" {
		t.Fatal(editor, err)
	}
	m, err := p.SessionCommands(ctx, "one", "/cd two  spaces 界")
	if err != nil || len(m.Choices) != 1 || !m.Choices[0].Action || !strings.Contains(m.Choices[0].Help, target) {
		t.Fatal(m, err)
	}
	if len(f.writes) != 0 {
		t.Fatal("review changed directory")
	}
	if err := p.ExecuteSessionCommand(ctx, "two", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("cross-session directory change")
	}
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	if len(f.writes) != 1 || len(f.writes[0]) != 2 || f.writes[0]["cwd"] != target || f.cwds["one"] != target || f.cwds["two"] != "/other" || f.sessions["one"].Model != "large" {
		t.Error("incorrect mutation", f.writes)
	}
	f.mu.Unlock()
	// The fixture intentionally keeps thread/read.cwd stale, like captured
	// metadata. /cwd must still report the running configuration's new path.
	m, err = p.SessionCommands(ctx, "one", "cwd")
	if err != nil || !strings.Contains(m.Choices[0].Help, target) {
		t.Fatal("stale cwd after change", m, err)
	}
}

func TestSessionDirectoryTargetsAndRevalidation(t *testing.T) {
	p, f, base := directoryFixture(t)
	ctx := context.Background()
	other := filepath.Join(base, "other")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(base, link); err != nil {
		t.Fatal(err)
	}
	m, err := p.SessionCommands(ctx, "one", "cd/"+url.PathEscape(link))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, link); err != nil {
		t.Fatal(err)
	}
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("retargeted symlink accepted")
	}
	home, _ := os.UserHomeDir()
	home, _ = filepath.EvalSymlinks(home)
	m, err = p.SessionCommands(ctx, "one", "cd/~")
	if err != nil || m.Choices[0].params["cwd"] != home {
		t.Fatal("home expansion", m, err)
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"", "%zz", url.PathEscape(file), url.PathEscape(filepath.Join(base, "missing")), "%00"} {
		if _, err := p.SessionCommands(ctx, "one", "cd/"+input); err == nil {
			t.Fatal("accepted invalid directory", input)
		}
	}
	for _, mode := range []string{"changed-directory", "active", "queued", "server-rejection", "pending-approval", "reconnect"} {
		t.Run(mode, func(t *testing.T) {
			p, f, base := directoryFixture(t)
			m, err := p.SessionCommands(ctx, "one", "cd/"+url.PathEscape(base))
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			switch mode {
			case "changed-directory":
				f.cwds["one"] = "/changed"
			case "active":
				f.commandStatus = "active"
			case "queued":
				f.queued = true
			case "server-rejection":
				f.fail = "cwd"
			}
			f.mu.Unlock()
			if mode == "pending-approval" {
				p.mu.Lock()
				p.contexts = map[string]*daemonContextState{"one": {requests: map[string]SessionContext{"r": {}}}}
				p.mu.Unlock()
			}
			if mode == "reconnect" {
				p.disconnect(nil)
			}
			if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
				t.Fatal("accepted changed directory context")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if mode != "server-rejection" && len(f.writes) != 0 {
				t.Fatal("unexpected directory write", f.writes)
			}
		})
	}
	f.mu.Lock()
	f.commandStatus = "active"
	f.mu.Unlock()
	m, err = p.SessionCommands(ctx, "one", "cd/"+url.PathEscape(base))
	if err != nil || m.Choices[0].Action {
		t.Fatal("active directory mutation available", m, err)
	}
	if _, err = p.SessionCommands(ctx, "one", "cwd"); err != nil {
		t.Fatal("cannot read active cwd", err)
	}
}

func TestSessionDirectoryAcknowledgementIsNotConfirmation(t *testing.T) {
	p, f, base := directoryFixture(t)
	other := filepath.Join(base, "next")
	if err := os.Mkdir(other, 0700); err != nil {
		t.Fatal(err)
	}
	f.ignoreCwd = true
	m, err := p.SessionCommands(context.Background(), "one", "cd/"+url.PathEscape(other))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("ignored cwd field reported as changed")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 1 || f.cwds["one"] != base {
		t.Fatal("unconfirmed write was retried", f.writes)
	}
}

func TestSessionDirectoryRejectsRemoteEnvironments(t *testing.T) {
	p, f, _ := directoryFixture(t)
	f.environments = []any{map[string]any{"environmentId": "remote"}}
	if _, err := p.SessionCommands(context.Background(), "one", "cd"); err == nil {
		t.Fatal("resolved remote directory on local filesystem")
	}
	if _, err := p.SessionCommands(context.Background(), "one", "cwd"); err != nil {
		t.Fatal("remote directory read unavailable", err)
	}
}

func TestSessionDirectoryWaitsForQueuedApplication(t *testing.T) {
	p, f, base := directoryFixture(t)
	target := filepath.Join(base, "later")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	f.ignoreCwd = true
	m, err := p.SessionCommands(context.Background(), "one", "cd/"+url.PathEscape(target))
	if err != nil {
		t.Fatal(err)
	}
	applied := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() {
		defer close(applied)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				f.mu.Lock()
				wrote := len(f.writes) == 1
				if wrote {
					f.cwds["one"] = target
				}
				f.mu.Unlock()
				if wrote {
					return
				}
			}
		}
	}()
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err != nil {
		t.Fatal(err)
	}
	<-applied
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 1 {
		t.Fatal("settings write retried")
	}
}

func TestSessionDirectoryScopedCataloguesFollowRunningCwd(t *testing.T) {
	p, f, base := directoryFixture(t)
	f.catalogues = map[string]map[string]any{
		"skills/list":            {"data": []any{map[string]any{"cwd": base, "skills": []any{map[string]any{"name": "current-project", "description": "Current directory help", "enabled": true}}}}},
		"permissionProfile/list": {"data": []any{map[string]any{"id": "safe", "description": "Read-only", "allowed": true}}},
	}
	ctx := context.Background()
	m, err := p.SessionCommands(ctx, "one", "skills")
	if err != nil || len(m.Choices) != 1 || m.Choices[0].Label != "current-project" {
		t.Fatal("used captured metadata cwd", m, err)
	}
	m, err = p.SessionCommands(ctx, "one", "permissions")
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.cwds["one"] = "/changed"
	f.mu.Unlock()
	if err := p.ExecuteSessionCommand(ctx, "one", m.Path, m.Revision, m.Choices[0].ID); err == nil {
		t.Fatal("directory-scoped setting ignored concurrent cwd change")
	}
}

func TestSessionDirectoryTelemetryTracksReadAndSettingsEvents(t *testing.T) {
	p, _, base := directoryFixture(t)
	snapshot, ok := p.Fetch(context.Background(), []string{"one"})
	if !ok || snapshot.WorkingDirectories["one"] != base {
		t.Fatal("initial running directory missing", snapshot, ok)
	}
	// Cwd is useful even if a model has no explicit reasoning effort.
	p.handleNotification("thread/settings/updated", []byte(`{"threadId":"one","threadSettings":{"cwd":"/changed","effort":null}}`))
	snapshot, ok = p.Fetch(context.Background(), []string{"one"})
	if !ok || snapshot.WorkingDirectories["one"] != "/changed" {
		t.Fatal("directory notification ignored", snapshot, ok)
	}
	snapshot.WorkingDirectories["one"] = "/modified-copy"
	snapshot, _ = p.Fetch(context.Background(), []string{"one"})
	if snapshot.WorkingDirectories["one"] != "/changed" {
		t.Fatal("snapshot aliases internal directory state")
	}
	p.disconnect(nil)
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.threadDirectories) != 0 {
		t.Fatal("directory observations retained after disconnect")
	}
}
