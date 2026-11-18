package prose

import (
	"strings"
	"testing"
)

func visible(r []ProseRun) string {
	var b strings.Builder
	for _, s := range r {
		b.WriteString(s.Text)
	}
	return b.String()
}

func TestMarkdownProjectsSourceWithoutInterpretingLiteralBlocks(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"**yes** and *maybe* and `**never**`", "yes and maybe and **never**"},
		{"- A **bold** item\n- And _another_", "- A bold item\n- And another"},
		{"Before\n\n> **quoted**\n\nAfter", "Before\n\n> quoted\n\nAfter"},
		{"**bold**\n\n```sh\necho **literal**\n```", "bold\n\n```sh\necho **literal**\n```"},
		{"**see** [docs](https://github.com/openai/codex) or <b>text</b>", "see [docs](https://github.com/openai/codex) or <b>text</b>"},
		{"***both*** and **[link](https://example.com)**", "both and [link](https://example.com)"},
		{"a **multi\nline** and ` two words `", "a multi\nline and two words"},
		{"![plot](image.png) and *now*", "![plot](image.png) and now"},
		{"<https://example.com> **done**", "<https://example.com> done"},
		{"**[link](https://example.com)**\nNext", "[link](https://example.com)\nNext"},
		{"**done**\n\n~~~md\n| example |\n|---|\n| *literal* |\n~~~", "done\n\n~~~md\n| example |\n|---|\n| *literal* |\n~~~"},
	} {
		doc := Parse(tc.src)
		if len(doc) != 1 || visible(doc[0].Runs) != tc.want {
			t.Errorf("%q: %#v, want %q", tc.src, doc, tc.want)
		}
	}
	doc := Parse("***both*** *italic* `<tag>`")
	if !doc[0].Runs[0].Bold || !doc[0].Runs[0].Italic || !doc[0].Runs[len(doc[0].Runs)-1].Code {
		t.Fatal(doc)
	}
	for _, s := range []string{"no formatting", "**in progress", "```\n*a*\n```", "a_b_c", "\\*literal\\*"} {
		if Parse(s) != nil {
			t.Errorf("expected literal %q", s)
		}
	}
}

func TestMarkdownTablesPreserveDataAndAdjacentLines(t *testing.T) {
	d := Parse("Intro\n| A | B |\n|---|--:|\n| `a\\|b` | **4** |\n| C | |\n\nAfter")
	if len(d) != 3 || visible(d[0].Runs) != "Intro" || visible(d[2].Runs) != "\nAfter" {
		t.Fatal(d)
	}
	b := d[1]
	if len(b.Rows) != 3 || b.Align[1] != "right" || visible(b.Rows[1][0]) != "a|b" || !b.Rows[1][0][0].Code || !b.Rows[1][1][0].Bold || visible(b.Rows[2][1]) != "" {
		t.Fatal(b)
	}
	d = Parse("| A |\n|---|\n\n**tail**")
	if len(d) != 2 || len(d[0].Rows) != 1 || visible(d[1].Runs) != "\ntail" {
		t.Fatal(d)
	}
	if Parse(strings.Repeat("|head", 500)+"|\n"+strings.Repeat("|---", 500)+"|") != nil {
		t.Fatal("unbounded table parsed")
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"***abc***", "| A |\n|-|\n|`a\\|b`|", "![a](b) **c**", "\n> x _z_"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, b := range Parse(s) {
			for _, r := range b.Rows {
				if len(r) != len(b.Align) {
					t.Fatal("ragged projection")
				}
			}
		}
	})
}
