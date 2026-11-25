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

func TestThreadLinkPreservesInlineFormattingAndSeparateLabels(t *testing.T) {
	const id = "550e8400-e29b-41d4-a716-446655440000"
	dest := "(codex://threads/" + id + ")"
	input := "See [Earlier ***work***, " + "`git status`" + ` and \[notes\]]` + dest + "[Again]" + dest + "."
	doc := Parse(input)
	if len(doc) != 1 || visible(doc[0].Runs) != "See Earlier work, git status and [notes]Again." {
		t.Fatalf("linked label was not projected as rendered text: %#v", doc)
	}
	starts := []string{}
	for _, r := range doc[0].Runs {
		if r.ThreadStart {
			starts = append(starts, r.Text)
		}
		switch r.Text {
		case "See ", ".":
			if r.ThreadID != "" {
				t.Fatalf("link consumed adjacent prose: %#v", r)
			}
		case "work":
			if !r.Bold || !r.Italic {
				t.Fatalf("nested emphasis was lost: %#v", r)
			}
		case "git status":
			if !r.Code {
				t.Fatalf("inline code was lost: %#v", r)
			}
		}
		if r.Text != "See " && r.Text != "." && r.ThreadID != id {
			t.Fatalf("linked fragment lost its target: %#v", r)
		}
	}
	if len(starts) != 2 || starts[0] != "Earlier " || starts[1] != "Again" {
		t.Fatalf("adjacent links sharing a target became a single label: %q", starts)
	}
}
