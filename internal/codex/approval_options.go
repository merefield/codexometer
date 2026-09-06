package codex

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Fixed-size, immutable value snapshots keep preview comparisons deterministic.
// Value selects an advertised option; Wire is the validated server payload.
type ApprovalOption struct{ Kind, Value, Wire, Detail string }

func (o ApprovalOption) GrantsPermission() bool {
	return o.Kind == "accept" || o.Kind == "acceptForSession" || o.Kind == "acceptWithExecpolicyAmendment"
}

func commandApprovalOptions(raw []json.RawMessage) ([8]ApprovalOption, string) {
	var out [8]ApprovalOption
	// Older servers omit this optional list. Keep a minimal legacy fallback;
	// never invent a persistent or session grant.
	if raw == nil {
		raw = []json.RawMessage{json.RawMessage(`"accept"`), json.RawMessage(`"cancel"`)}
	}
	var names []string
	n := 0
	for _, r := range raw {
		var name string
		var o ApprovalOption
		if json.Unmarshal(r, &name) == nil {
			switch name {
			case "accept", "acceptForSession", "decline", "cancel":
				wire, _ := json.Marshal(name)
				o = ApprovalOption{Kind: name, Value: name, Wire: string(wire)}
			default:
				name = "unsupported-string"
			}
		} else {
			name = "unsupported-object"
			var object map[string]json.RawMessage
			if json.Unmarshal(r, &object) == nil && len(object) == 1 {
				if payload, ok := object["acceptWithExecpolicyAmendment"]; ok {
					name = "acceptWithExecpolicyAmendment"
					var amendment struct {
						Prefix []string `json:"execpolicy_amendment"`
					}
					decoder := json.NewDecoder(bytes.NewReader(payload))
					decoder.DisallowUnknownFields()
					if decoder.Decode(&amendment) == nil && len(amendment.Prefix) > 0 {
						prefix, _ := json.Marshal(amendment.Prefix)
						valid := len(prefix) <= 1024
						for _, part := range amendment.Prefix {
							valid = valid && part != "" && SanitizeSessionContext(part) == part
						}
						if valid {
							wire, _ := json.Marshal(map[string]any{"acceptWithExecpolicyAmendment": amendment})
							o = ApprovalOption{Kind: name, Value: string(wire), Wire: string(wire), Detail: string(prefix)}
						} else {
							name = "unsupported-execpolicy"
						}
					} else {
						name = "unsupported-execpolicy"
					}
				}
			}
		}
		if len(names) < 16 {
			names = append(names, name)
		}
		if o.Kind != "" && n < len(out) {
			duplicate := false
			for _, old := range out {
				duplicate = duplicate || old.Value == o.Value
			}
			if !duplicate {
				out[n] = o
				n++
			}
		}
	}
	if len(raw) > 16 {
		names = append(names, "…")
	}
	return out, strings.Join(names, ", ")
}
