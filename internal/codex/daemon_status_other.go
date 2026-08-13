//go:build !unix

package codex

func newSessionStatusProvider(string) sessionStatusProvider { return nil }
