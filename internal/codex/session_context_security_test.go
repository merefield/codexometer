package codex

import (
	"testing"
	"unicode"
	"unicode/utf8"
)

func FuzzSessionContextSanitization(f *testing.F) {
	for _, seed := range []string{
		"ordinary 日本語\nsecond line", "\xff\xfe\x00bad", "\x1b]52;c;payload\a", "\x1bPdevice payload\x1b\\", "\x1b[2J\x1b[?1049h", "\u009b2J\u009d52;c;payload\u009c", "left\u202eright\u2066", "\x1b]8;;https://evil.invalid\x1b\\link\x1b]8;;\x1b\\",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		output := SanitizeSessionContext(input)
		if !utf8.ValidString(output) || len([]rune(output)) > sessionContextLimit {
			t.Fatal("invalid or unbounded output")
		}
		for _, r := range output {
			if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) {
				t.Fatalf("unsafe rune %U", r)
			}
		}
		if SanitizeSessionContext(output) != output {
			t.Fatal("sanitization not idempotent")
		}
	})
}
