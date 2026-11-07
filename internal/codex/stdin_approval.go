package codex

import (
	"crypto/rand"
	"encoding/json"
	"strconv"
	"strings"
)

type stdinApprovalParams struct {
	Command     string            `json:"command"`
	Directory   string            `json:"cwd"`
	Reason      string            `json:"reason"`
	ApprovalID  string            `json:"approvalId"`
	Environment string            `json:"environmentId"`
	Decisions   []json.RawMessage `json:"availableDecisions"`
	Permissions json.RawMessage   `json:"additionalPermissions"`
	Network     json.RawMessage   `json:"networkApprovalContext"`
}

func configureStdinApproval(c *SessionContext, raw json.RawMessage) {
	c.Kind, c.ApprovalBlocked = SessionContextApproval, "command-format"
	c.Text = "Terminal input requires review in Codex."
	var p stdinApprovalParams
	if len(raw) > approvalTextLimit {
		c.ApprovalBlocked = "truncated"
		return
	}
	if json.Unmarshal(raw, &p) != nil {
		return
	}
	c.Text += "\n" + p.Reason + "\n" + p.Command
	if c.TurnID == "" || c.ItemID == "" || p.ApprovalID == "" || !approvalTextDisplayable(p.ApprovalID) {
		c.ApprovalBlocked = "missing-identity"
		return
	}
	if len(p.Permissions) > 0 && string(p.Permissions) != "null" {
		c.ApprovalBlocked = "permissions"
		return
	}
	if len(p.Network) > 0 && string(p.Network) != "null" {
		c.ApprovalBlocked = "network"
		return
	}
	if p.Directory == "" {
		c.ApprovalBlocked = "missing-directory"
		return
	}
	terminal, input, valid := terminalApprovalInput(p.Command)
	if !valid || strings.ContainsRune(input, 0) {
		return
	}
	c.Text = "Terminal: " + terminal + "\nInput (escaped): " + strconv.QuoteToASCII(input) + "\nStart directory: " + p.Directory + "\nEnvironment: " + p.Environment + "\n" + p.Reason
	if !approvalTextFits(c.Text) {
		c.ApprovalBlocked = "truncated"
		return
	}
	if !approvalTextDisplayable(c.Text) {
		c.ApprovalBlocked = "sanitised"
		return
	}
	// Input approval never grants authority for later inputs or commands.
	parsed, _ := commandApprovalOptions(p.Decisions)
	var options [8]ApprovalOption
	n := 0
	for _, option := range parsed {
		if !oneTimeApproval(option.Kind) {
			continue
		}
		switch option.Kind {
		case "accept":
			option.Kind = "acceptStdin"
		case "cancel":
			option.Kind = "cancelStdin"
		}
		options[n] = option
		n++
	}
	if p.Decisions == nil || n == 0 {
		c.ApprovalBlocked = "decisions"
		return
	}
	c.ActionDetails = ApprovalActionDetails{Kind: "stdin", Target: terminal, Input: input, Directory: p.Directory, Environment: p.Environment, Justification: p.Reason}
	c.ApprovalOptions = options
	c.ApprovalToken, c.ApprovalBlocked = rand.Text(), ""
}

// Codex 0.167 presents stdin as shlex::try_join of exactly four arguments:
// write_stdin, --session-id, a decimal terminal ID, and the literal input.
// Decode that presentation only. This is not a shell evaluator; expansions,
// substitutions, operators and extra arguments are never interpreted.
func terminalApprovalInput(command string) (string, string, bool) {
	rest, ok := strings.CutPrefix(command, "write_stdin --session-id ")
	if !ok {
		return "", "", false
	}
	terminal, encoded, ok := strings.Cut(rest, " ")
	number, err := strconv.ParseUint(terminal, 10, 64)
	if !ok || err != nil || strconv.FormatUint(number, 10) != terminal {
		return "", "", false
	}
	input, valid := quotedApprovalArgument(encoded)
	return terminal, input, valid
}

func quotedApprovalArgument(encoded string) (string, bool) {
	var out strings.Builder
	var quote byte
	if encoded == "" {
		return "", false
	}
	for i := 0; i < len(encoded); i++ {
		ch := encoded[i]
		if quote == '\'' {
			if ch == '\'' {
				quote = 0
			} else {
				out.WriteByte(ch)
			}
			continue
		}
		if ch == '\\' {
			if i+1 == len(encoded) {
				return "", false
			}
			next := encoded[i+1]
			if next == '\n' || next == '\r' {
				return "", false
			}
			if quote == '"' && !strings.ContainsRune("\\\"$`", rune(next)) {
				out.WriteByte(ch)
				continue
			}
			out.WriteByte(next)
			i++
			continue
		}
		if ch == '"' || ch == '\'' && quote == 0 {
			if quote == ch {
				quote = 0
			} else if quote == 0 {
				quote = ch
			} else {
				out.WriteByte(ch)
			}
			continue
		}
		if quote == 0 && strings.ContainsRune(" \t\r\n|&;()<>#*?[]{}~", rune(ch)) || ch == '$' || ch == '`' {
			return "", false
		}
		out.WriteByte(ch)
	}
	return out.String(), quote == 0
}
