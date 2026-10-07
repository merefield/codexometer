package codex

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Patches stay in memory, scoped to an exact live item. Never read the local
// filesystem to reconstruct what a remote approval might authorise.
const fileApprovalLimit = 64 * 1024

type FileChange struct {
	Path string `json:"path"`
	Kind struct {
		Type     string `json:"type"`
		MovePath string `json:"move_path"`
	} `json:"kind"`
	Diff string `json:"diff"`
}

func safePatchText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if (unicode.IsControl(r) && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func validatedFileChanges(raw json.RawMessage) string {
	if len(raw) > fileApprovalLimit || !utf8.Valid(raw) {
		return ""
	}
	var changes []FileChange
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&changes) != nil || len(changes) == 0 || len(changes) > 64 {
		return ""
	}
	var fields []map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	for _, f := range fields {
		if len(f["diff"]) == 0 || string(f["diff"]) == "null" {
			return ""
		}
	}
	for _, c := range changes {
		if strings.TrimSpace(c.Path) == "" || strings.ContainsAny(c.Path+c.Kind.MovePath, "\n\t") || !safePatchText(c.Path+c.Kind.MovePath+c.Diff) {
			return ""
		}
		switch c.Kind.Type {
		case "add", "delete", "update":
		default:
			return ""
		}
	}
	data, _ := json.Marshal(changes)
	return string(data)
}

func configureFileApproval(c *SessionContext, grantRoot string, state *daemonContextState) {
	c.ApprovalToken = ""
	c.ApprovalOptions = [8]ApprovalOption{}
	c.ApprovalBlocked = "file-change"
	if c.FileChanges == "" {
		return
	}
	if c.TurnID == "" || c.ItemID == "" || state.ended || state.activeTurn != "" && c.TurnID != state.activeTurn {
		c.ApprovalBlocked = "missing-identity"
		return
	}
	// Root grants have unstable semantics. Keep them in Codex, rather than
	// suggesting that approval once authorises only the displayed patch.
	if grantRoot != "" {
		c.ApprovalBlocked = "permissions"
		return
	}
	if SanitizeSessionContext(c.Text) != c.Text {
		c.ApprovalBlocked = "sanitised"
		return
	}
	c.ApprovalOptions, c.ApprovalDecisions = commandApprovalOptions([]json.RawMessage{json.RawMessage(`"accept"`), json.RawMessage(`"decline"`), json.RawMessage(`"cancel"`)})
	c.ApprovalBlocked, c.ApprovalToken = "", rand.Text()
}

func (s *daemonContextState) capturePatch(turn, item string, raw json.RawMessage) {
	if turn == "" || item == "" || s.ended || s.activeTurn != "" && turn != s.activeTurn {
		return
	}
	if s.patches == nil {
		s.patches = map[string]string{}
	}
	key := turn + "/" + item
	patch := validatedFileChanges(raw)
	if _, exists := s.patches[key]; exists || len(s.patches) < 16 {
		s.patches[key] = patch
	}
	// Any changed patch invalidates an already displayed/confirmed capability.
	// A new approval request is required; never silently broaden a grant.
	for id, c := range s.requests {
		if c.TurnID == turn && c.ItemID == item && c.Kind == SessionContextApproval && c.FileChanges != patch {
			c.FileChanges, c.ApprovalToken, c.ApprovalBlocked = patch, "", "file-change"
			s.requests[id] = c
		}
	}
}

type FileDiffLine struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
}

// Count logical changed lines, not wrapped display rows or patch headers.
func FileDiffTotals(lines []FileDiffLine) (added, removed int) {
	for _, line := range lines {
		switch line.Kind {
		case "addition":
			added++
		case "removal":
			removed++
		}
	}
	return
}

var diffHunk = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// FileDiffLines is shared by both front ends. Add/delete diffs are whole-file
// content; update diffs are unified patches. Prefixes are never inferred from
// file contents for add/delete operations.
func FileDiffLines(encoded string) (out []FileDiffLine) {
	var changes []FileChange
	if json.Unmarshal([]byte(encoded), &changes) != nil {
		return nil
	}
	for _, c := range changes {
		out = append(out, FileDiffLine{Text: strings.ToUpper(c.Kind.Type) + " // " + c.Path, Kind: "heading"})
		if c.Kind.MovePath != "" {
			out = append(out, FileDiffLine{Text: "→ " + c.Kind.MovePath, Kind: "heading"})
		}
		if c.Diff == "" {
			continue
		}
		old, next := 0, 0
		if c.Kind.Type == "add" {
			next = 1
		}
		if c.Kind.Type == "delete" {
			old = 1
		}
		for _, text := range strings.Split(strings.TrimSuffix(c.Diff, "\n"), "\n") {
			line := FileDiffLine{Text: strings.ReplaceAll(text, "\t", "    "), Kind: "body"}
			switch {
			case c.Kind.Type == "add":
				line.Kind, line.New, line.Text = "addition", next, "+"+line.Text
				next++
			case c.Kind.Type == "delete":
				line.Kind, line.Old, line.Text = "removal", old, "-"+line.Text
				old++
			case diffHunk.MatchString(text):
				m := diffHunk.FindStringSubmatch(text)
				old, _ = strconv.Atoi(m[1])
				next, _ = strconv.Atoi(m[2])
				line.Kind = "metadata"
			case strings.HasPrefix(text, "+") && next > 0:
				line.Kind, line.New = "addition", next
				next++
			case strings.HasPrefix(text, "-") && old > 0:
				line.Kind, line.Old = "removal", old
				old++
			case strings.HasPrefix(text, " ") && (old > 0 || next > 0):
				line.Old, line.New = old, next
				old++
				next++
			}
			out = append(out, line)
		}
	}
	return out
}

func (l FileDiffLine) NumberedText() string {
	if l.Kind == "heading" || l.Kind == "metadata" {
		return l.Text
	}
	old, next := "", ""
	if l.Old > 0 {
		old = strconv.Itoa(l.Old)
	}
	if l.New > 0 {
		next = strconv.Itoa(l.New)
	}
	return fmt.Sprintf("%5s %5s │ %s", old, next, l.Text)
}
