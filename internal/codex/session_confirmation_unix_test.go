//go:build unix

package codex

import "testing"

func TestOnlyAllowlistedSessionChangesAreReversible(t *testing.T) {
	for _, tc := range []struct {
		method string
		params map[string]any
		want   bool
	}{
		{"thread/name/set", map[string]any{"threadId": "one", "name": "New name"}, true},
		{"thread/settings/update", map[string]any{"threadId": "one", "model": "m", "effort": "high", "serviceTier": nil}, true},
		{"thread/settings/update", map[string]any{"threadId": "one", "collaborationMode": map[string]any{}}, true},
		{"thread/settings/update", map[string]any{"threadId": "one", "serviceTier": "priority"}, true},
		{"thread/settings/update", map[string]any{"threadId": "one", "permissions": "full"}, false},
		{"thread/settings/update", map[string]any{"threadId": "one", "model": "m", "permissions": "full"}, false},
		{"thread/settings/update", map[string]any{"threadId": "one", "cwd": "/new"}, false},
		{"thread/settings/update", map[string]any{"threadId": "one", "futureField": "value"}, false},
		{"thread/settings/update", map[string]any{"threadId": "one"}, false},
		{"thread/settings/update", map[string]any{"model": "m"}, false},
		{"future/write", map[string]any{"threadId": "one"}, false},
	} {
		menu := normalizeCommandMenu(SessionCommandMenu{Choices: []SessionCommandChoice{{Label: "Option", method: tc.method, params: tc.params}}})
		if len(menu.Choices) != 1 || menu.Choices[0].Reversible != tc.want {
			t.Fatal("wrong reversible action classification", tc.method, tc.params, menu)
		}
	}
	menu := normalizeCommandMenu(SessionCommandMenu{Choices: []SessionCommandChoice{{Label: "Read only", Reversible: true}}})
	if menu.Choices[0].Reversible || menu.Choices[0].Action {
		t.Fatal("browse-only metadata became an action")
	}
}
