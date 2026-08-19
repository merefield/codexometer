package codex

import "github.com/merefield/codexometer/internal/version"

func codexometerClientInfo() map[string]string {
	return map[string]string{
		"name":    "codexometer",
		"title":   "Codexometer",
		"version": version.Current(),
	}
}
