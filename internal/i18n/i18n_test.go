package i18n

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPresentationKeysHaveCatalogueEntries(t *testing.T) {
	data, err := files.ReadFile("locales/en-GB.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries map[string]string
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir("../ui", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || (selector.Sel.Name != "Text" && selector.Sel.Name != "Format") {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "i18n" {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Error(err)
			} else if _, ok := entries[key]; !ok {
				t.Errorf("%s: missing catalogue entry for %q", path, key)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLanguageSelection(t *testing.T) {
	for input, want := range map[string]string{
		"sv": "sv", "sv-SE": "sv", "sv-FI": "sv",
		"nb": "nb", "nb-NO": "nb", "no": "nb", "no-NO": "nb",
		"tr": "tr", "tr-TR": "tr", "et": "et", "et-EE": "et", "fi": "fi", "fi-FI": "fi",
		"pt": "pt-BR", "pt-BR": "pt-BR", "pt-PT": "pt-BR", "pt-AO": "pt-BR",
		"da": "da", "da-DK": "da", "da-GL": "da",
	} {
		if got := New(input).Code(); got != want {
			t.Errorf("%q: %q, want %q", input, got, want)
		}
	}
	for input, want := range map[string]string{"": "en-GB", "en": "en-GB", "en-US": "en-GB", "en-GB": "en-GB", "nl-NL": "nl", "de-AT": "de", "fr-CA": "fr", "it": "it", "es-MX": "es", "ru-RU": "ru", "ja-JP": "ja", "zh-CN": "zh-Hans", "zh-Hans": "zh-Hans", "zz": "en-GB", "not a language": "en-GB", "  de  ": "de"} {
		if got := New(input).Code(); got != want {
			t.Errorf("%q: %q, want %q", input, got, want)
		}
	}
}

func TestCataloguesAndEnglishCompatibility(t *testing.T) {
	var english map[string]string
	data, _ := files.ReadFile("locales/en-GB.json")
	if err := json.Unmarshal(data, &english); err != nil {
		t.Fatal(err)
	}
	verbs := regexp.MustCompile(`%(?:\[[0-9]+\])?[-+# 0]*[0-9]*(?:\.[0-9]+)?[a-zA-Z%]`)
	keys := regexp.MustCompile(`\([A-Z0-9]\)`)
	for _, code := range codes {
		var entries map[string]string
		data, err := files.ReadFile("locales/" + code + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &entries); err != nil {
			t.Fatal(err)
		}
		if len(entries) != len(english) {
			t.Errorf("%s: incomplete catalogue", code)
		}
		tr := New(code)
		for key := range english {
			value, ok := entries[key]
			if !ok || value == "" || !utf8.ValidString(value) || strings.ContainsAny(value, "\x1b\x00\r") {
				t.Errorf("%s invalid entry %q", code, key)
				continue
			}
			if !slices.Equal(verbs.FindAllString(key, -1), verbs.FindAllString(value, -1)) {
				t.Errorf("%s changed placeholders in %q", code, key)
			}
			if !slices.Equal(keys.FindAllString(key, -1), keys.FindAllString(value, -1)) {
				t.Errorf("%s changed hotkeys in %q", code, key)
			}
			if got := tr.Text(key); got != value {
				t.Errorf("%s literal lookup %q: %q, want %q", code, key, got, value)
			}
			if code == "en-GB" && value != key {
				t.Errorf("English presentation changed for %q", key)
			}
		}
	}
	for _, code := range []string{"", "en", "en-GB", "en-US", "invalid language"} {
		tr := New(code)
		if got, want := tr.Format("USED %3d%%", 25), fmt.Sprintf("USED %3d%%", 25); got != want {
			t.Errorf("English formatting changed: %q vs %q", got, want)
		}
		if got := tr.Format("%08.2f", 1234.5); got != fmt.Sprintf("%08.2f", 1234.5) {
			t.Errorf("English numeric format changed: %q", got)
		}
	}
}

func TestLiteralFallbackAndTranslations(t *testing.T) {
	for code, want := range map[string]string{"sv": "ANVÄNDNING", "nb": "BRUK", "tr": "KULLANIM", "et": "KASUTUS", "fi": "KÄYTTÖ", "pt-BR": "USO", "da": "FORBRUG"} {
		if got := New(code).Text("USAGE"); got != want {
			t.Errorf("%s translation: %q, want %q", code, got, want)
		}
	}
	for _, code := range codes {
		tr := New(code)
		if got := tr.Text("untranslated 50% complete"); got != "untranslated 50% complete" {
			t.Errorf("%s fallback: %q", code, got)
		}
		if strings.Contains(tr.Format("USED %3d%%", 25), "%!") {
			t.Errorf("%s invalid formatting", code)
		}
	}
	if got := New("ja").Text("USAGE"); got != "使用状況" {
		t.Errorf("Japanese translation: %q", got)
	}
	if got := New("zh-Hans").Text("USAGE"); got != "用量" {
		t.Errorf("Chinese translation: %q", got)
	}
}

func TestWindowNameUsesLocalePluralRules(t *testing.T) {
	for _, test := range []struct{ code, name, want string }{
		{"en-GB", "1 WEEK", "1 WEEK"}, {"en-GB", "5 HOURS", "5 HOURS"},
		{"de", "1 WEEK", "1 WOCHE"}, {"de", "2 WEEKS", "2 WOCHEN"},
		{"ru", "1 HOUR", "1 ЧАС"}, {"ru", "2 HOURS", "2 ЧАСА"}, {"ru", "5 HOURS", "5 ЧАСОВ"}, {"ru", "21 HOURS", "21 ЧАС"},
		{"ja", "5 HOURS", "5 時間"}, {"zh-Hans", "1 DAY", "1 天"},
		{"sv", "1 HOUR", "1 TIMME"}, {"sv", "2 HOURS", "2 TIMMAR"},
		{"nb", "1 WEEK", "1 UKE"}, {"nb", "2 WEEKS", "2 UKER"},
		{"tr", "1 DAY", "1 GÜN"}, {"tr", "2 DAYS", "2 GÜN"},
		{"et", "1 MINUTE", "1 MINUT"}, {"et", "2 MINUTES", "2 MINUTIT"},
		{"fi", "1 WEEK", "1 VIIKKO"}, {"fi", "2 WEEKS", "2 VIIKKOA"}, {"fi", "0 HOURS", "0 TUNTIA"},
		{"pt-BR", "1 WEEK", "1 SEMANA"}, {"pt-BR", "2 WEEKS", "2 SEMANAS"}, {"pt-BR", "0 HOURS", "0 HORA"},
		{"da", "1 WEEK", "1 UGE"}, {"da", "2 WEEKS", "2 UGER"}, {"da", "0 HOURS", "0 TIMER"},
		{"fr", "custom-model-id", "custom-model-id"},
	} {
		if got := New(test.code).WindowName(test.name); got != test.want {
			t.Errorf("%s %q: %q, want %q", test.code, test.name, got, test.want)
		}
	}
}
