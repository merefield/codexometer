package codex

import (
	"net/url"
	"strings"
	"testing"
)

func TestSessionCommandLiteral(t *testing.T) {
	for _, tc := range []struct {
		text    string
		command bool
		literal string
	}{
		{"/model", true, "/model"}, {" /unknown x", true, " /unknown x"},
		{"//tmp/example", false, "/tmp/example"}, {"  //help", false, "  /help"},
		{"hello /model", false, "hello /model"}, {"", false, ""},
	} {
		if IsSessionCommand(tc.text) != tc.command || literalSessionText(tc.text) != tc.literal {
			t.Fatalf("unexpected classification/escape: %q", tc.text)
		}
	}
}

func TestSessionCommandPathDecodedLimits(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		valid      bool
	}{
		{"CJK rename boundary", "rename/" + url.PathEscape(strings.Repeat("界", 512)), true},
		{"four-byte rename boundary", "/rename/" + url.PathEscape(strings.Repeat("😀", 512)), true},
		{"direct Unicode rename", "/rename " + strings.Repeat("界", 512), true},
		{"directory spaces boundary", "cd/" + url.PathEscape(strings.Repeat(" ", 1024)), true},
		{"four-byte directory boundary", "/cd/" + url.PathEscape(strings.Repeat("😀", 1024)), true},
		{"direct Unicode directory", "/cd " + strings.Repeat("界", 1024), true},
		{"rename over limit", "rename/" + url.PathEscape(strings.Repeat("界", 513)), false},
		{"direct rename over limit", "/rename " + strings.Repeat("界", 513), false},
		{"directory over limit", "cd/" + url.PathEscape(strings.Repeat("😀", 1025)), false},
		{"direct directory over limit", "/cd " + strings.Repeat("界", 1025), false},
		{"bad escaping", "rename/%zz", false},
		{"invalid encoded UTF-8", "cd/%FF", false},
		{"invalid plain UTF-8", "rename/\xff", false},
		{"other command boundary", "model/" + strings.Repeat("x", 2042), true},
		{"other command over limit", "model/" + strings.Repeat("x", 2043), false},
		{"oversized transport", strings.Repeat("x", SessionCommandPathLimit+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidSessionCommandPath(tc.path); got != tc.valid {
				t.Fatalf("valid=%v want %v (encoded bytes=%d)", got, tc.valid, len(tc.path))
			}
		})
	}
}
