//go:build !darwin

package main

func appleTerminalSupportsTrueColor() bool { return false }
