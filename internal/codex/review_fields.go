package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// reviewFields renders an object's fields without changing their types, merging
// duplicate keys, rounding numbers, or interpreting strings as markup. Limits
// apply to the whole inspection: never offer an action on a partial outline.
func reviewFields(raw json.RawMessage, labels map[string]string) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > approvalTextLimit || raw[0] != '{' || !json.Valid(raw) {
		return "", false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var out strings.Builder
	var visit func(string, int) bool
	visit = func(label string, depth int) bool {
		if depth > 16 || out.Len() > approvalTextLimit {
			return false
		}
		token, err := d.Token()
		if err != nil {
			return false
		}
		if depth > 0 {
			out.WriteString(strings.Repeat("  ", depth-1) + label + ":")
		}
		switch v := token.(type) {
		case json.Delim:
			obj := v == '{'
			if !d.More() {
				if obj {
					out.WriteString(" {}\n")
				} else {
					out.WriteString(" []\n")
				}
			} else if depth > 0 {
				out.WriteByte('\n')
			}
			seen := map[string]bool{}
			for i := 0; d.More(); i++ {
				var name string
				if obj {
					k, err := d.Token()
					key, ok := k.(string)
					if err != nil || !ok || seen[key] || strings.ContainsRune(key, utf8.RuneError) {
						return false
					}
					seen[key] = true
					name = labels[key]
					if name == "" {
						// Quote unusual keys so an argument cannot forge a heading
						// or masquerade as an array index or translated label.
						name = strconv.Quote(key)
						if labels == nil && simpleReviewKey(key) {
							name = key
						}
					}
				} else {
					name = "[" + strconv.Itoa(i+1) + "]"
				}
				if !visit(name, depth+1) {
					return false
				}
			}
			_, err = d.Token()
		case string:
			// encoding/json replaces invalid UTF-8 and lone UTF-16 surrogates
			// with U+FFFD. Never describe a lossy decoding as an exact review.
			if strings.ContainsRune(v, utf8.RuneError) {
				return false
			}
			out.WriteString(" " + strconv.Quote(v) + "\n")
		case nil:
			out.WriteString(" null\n")
		default: // boolean or json.Number (original numeric spelling)
			fmt.Fprintln(&out, "", v)
		}
		return err == nil && out.Len() <= approvalTextLimit
	}
	if !visit("", 0) {
		return "", false
	}
	return strings.TrimSpace(out.String()), true
}

func simpleReviewKey(s string) bool {
	return s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-')
	}) < 0
}

var permissionFieldNames = map[string]string{
	"network": "Network access", "enabled": "Enabled", "fileSystem": "File system",
	"read": "Read paths", "write": "Write paths", "entries": "Access rules", "access": "Access",
	"path": "Path", "type": "Type", "pattern": "Pattern", "value": "Value", "kind": "Kind",
	"subpath": "Subpath", "globScanMaxDepth": "Maximum glob depth", "host": "Host", "protocol": "Protocol",
}

// Only for requests that must be reviewed in Codex. Unknown, ambiguous or
// oversized data never becomes an actionable substitute for a native review.
func observedScope(raw json.RawMessage) string {
	if fields, ok := reviewFields(raw, permissionFieldNames); ok {
		return fields
	}
	return incompleteScope
}

const incompleteScope = "Cannot display the complete scope. Inspect the original request in Codex."
