package prose

import (
	"strings"
	"testing"
)

func TestThreadLinkExactDestinationAndMarkdownScope(t *testing.T) {
	const id = "550e8400-e29b-41d4-a716-446655440000"
	url := "codex://threads/" + id
	link := "[Earlier **work**](" + url + ")"
	for _, input := range []string{"Open " + link + " please.", "| Link |\n| --- |\n| " + link + " |", "- " + link} {
		if !HasThreadLinks(Parse(input)) {
			t.Errorf("valid link not projected: %q", input)
		}
	}
	for _, input := range []string{"`" + link + "`", "```md\n" + link + "\n```", "    " + link, "!" + link, "\\" + link, "[link](" + url + "#extra)", "[link](https://example.test/?x=" + url + ")"} {
		if HasThreadLinks(Parse(input)) {
			t.Errorf("inert text became an action: %q", input)
		}
	}
	for _, candidate := range []string{url + "?a=1", url + "/", strings.ToUpper(url), "codex://threads@host/" + id, "codex://threads/%35" + id[1:]} {
		if ThreadID(candidate) != "" {
			t.Errorf("invalid destination accepted: %s", candidate)
		}
	}
}
