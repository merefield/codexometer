//go:build unix

package codex

import (
	"context"
	"strings"
	"testing"
)

func TestThreadUsageCoverageUsesEffectiveFeatureInventory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    any
		next    any
		blocked bool
		invalid bool
	}{
		{name: "older inventory", data: []any{}},
		{name: "async enabled", data: []any{map[string]any{"name": "async_compaction", "enabled": true}}, blocked: true},
		{name: "disabled", data: []any{map[string]any{"name": "async_compaction", "enabled": false}}},
		{name: "token budget disables async path", data: []any{map[string]any{"name": "async_compaction", "enabled": true}, map[string]any{"name": "token_budget", "enabled": true}}},
		{name: "missing enablement", data: []any{map[string]any{"name": "async_compaction"}}, invalid: true},
		{name: "missing data", invalid: true},
		{name: "repeated cursor", data: []any{}, next: "same", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, f := newQuotaDaemon(t)
			f.catalogues = map[string]map[string]any{"experimentalFeature/list": {"data": tc.data, "nextCursor": tc.next}}
			if err := p.ensureConnected(context.Background()); err != nil {
				t.Fatal(err)
			}
			issue, err := p.threadUsageCoverage(context.Background(), "one")
			if (err != nil) != tc.invalid || strings.Contains(issue, "background compaction") != tc.blocked {
				t.Fatal(issue, err)
			}
		})
	}
}
