package statusline

import (
	"reflect"
	"strings"
	"testing"
)

func TestSelectionAndSanitizedOneLineValues(t *testing.T) {
	values := Values(Data{Model: "model\x1b[31m", Effort: "high", Speed: "fast", Tokens: 1200, Directory: "/work\nproject"})
	if got := Text(nil, values); got != "model high · fast · 1.2K tokens · /work project" {
		t.Fatal(got)
	}
	if Text([]string{}, values) != "" {
		t.Fatal("empty selection must hide footer")
	}
	if got := Normalize([]string{"used-tokens", "invalid", "model", "used-tokens"}); !reflect.DeepEqual(got, []string{"used-tokens", "model"}) {
		t.Fatal(got)
	}
	if got := Text([]string{"thread-name", "reasoning", "model"}, values); got != "high · model" || strings.ContainsAny(got, "\n\x1b") {
		t.Fatal(got)
	}
}
