package codex

import "testing"

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
