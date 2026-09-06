// Package i18n owns process-wide, startup-selected presentation language.
// Protocol identifiers, benchmark prompts, and persisted preference keys are
// deliberately outside the translation boundary.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/message/catalog"
)

const EnvironmentVariable = "CODEXOMETER_LANG"

//go:embed locales/*.json
var files embed.FS

var codes = []string{"en-GB", "nl", "de", "fr", "it", "es", "ru", "ja", "zh-Hans"}
var tags = []language.Tag{language.BritishEnglish, language.Dutch, language.German, language.French, language.Italian, language.Spanish, language.Russian, language.Japanese, language.SimplifiedChinese}
var matcher = language.NewMatcher(tags)
var textCatalog, formatCatalog = loadCatalogues()
var current = New(os.Getenv(EnvironmentVariable))

type Translator struct {
	code         string
	text, format *message.Printer
}

// New accepts BCP 47 language tags, matching regional variants to supported
// languages. An unset, malformed, or unsupported value falls back to en-GB.
// No implicit LANG/LC_ALL detection: English remains the explicit default.
func New(value string) *Translator {
	index := 0
	if tag, err := language.Parse(strings.TrimSpace(value)); err == nil && strings.TrimSpace(value) != "" {
		_, matched, confidence := matcher.Match(tag)
		if confidence != language.No {
			index = matched
		}
	}
	return &Translator{code: codes[index], text: message.NewPrinter(tags[index], message.Catalog(textCatalog)), format: message.NewPrinter(tags[index], message.Catalog(formatCatalog))}
}

func (t *Translator) Code() string { return t.code }
func Code() string                 { return current.Code() }

// Text translates a literal before layout measurement, never a rendered ANSI
// screen or backend payload. Literal percent signs are not format directives.
func (t *Translator) Text(key string) string {
	if t.code == "en-GB" {
		return key
	}
	return t.text.Sprintf(message.Key(key, strings.ReplaceAll(key, "%", "%%")))
}

func (t *Translator) Format(key string, args ...any) string {
	// Preserve the established English presentation byte-for-byte, including
	// its numeric grouping, padding and precision conventions.
	if t.code == "en-GB" {
		return fmt.Sprintf(key, args...)
	}
	return t.format.Sprintf(message.Key(key, key), args...)
}

func Text(key string) string                { return current.Text(key) }
func Format(key string, args ...any) string { return current.Format(key, args...) }

// WindowName translates only the app's canonical quota-window names, not
// arbitrary server-provided model names or identifiers.
func WindowName(name string) string { return current.WindowName(name) }
func (t *Translator) WindowName(name string) string {
	if t.code == "en-GB" {
		return name
	}
	fields := strings.Fields(name)
	if len(fields) == 2 {
		count, err := strconv.ParseInt(fields[0], 10, 64)
		unit := strings.TrimSuffix(fields[1], "S")
		if err == nil && (unit == "MINUTE" || unit == "HOUR" || unit == "DAY" || unit == "WEEK") {
			return t.Format("window."+unit, count)
		}
	}
	return t.Text(name)
}

func loadCatalogues() (*catalog.Builder, *catalog.Builder) {
	literals := catalog.NewBuilder(catalog.Fallback(language.BritishEnglish))
	formats := catalog.NewBuilder(catalog.Fallback(language.BritishEnglish))
	var units map[string]map[string]map[string]string
	unitData, err := files.ReadFile("locales/units.json")
	if err != nil {
		panic(err)
	}
	if err = json.Unmarshal(unitData, &units); err != nil {
		panic(err)
	}
	for i, code := range codes {
		data, err := files.ReadFile("locales/" + code + ".json")
		if err != nil {
			panic(err)
		}
		var entries map[string]string
		if err = json.Unmarshal(data, &entries); err != nil {
			panic(err)
		}
		for key, value := range entries {
			if err = literals.SetString(tags[i], key, strings.ReplaceAll(value, "%", "%%")); err != nil {
				panic(err)
			}
			if err = formats.SetString(tags[i], key, value); err != nil {
				panic(err)
			}
		}
		for unit, forms := range units[code] {
			var cases []any
			for _, form := range []string{"one", "few", "many", "other"} {
				if value, ok := forms[form]; ok {
					cases = append(cases, form, value)
				}
			}
			if err = formats.Set(tags[i], "window."+unit, plural.Selectf(1, "%d", cases...)); err != nil {
				panic(err)
			}
		}
	}
	return literals, formats
}
