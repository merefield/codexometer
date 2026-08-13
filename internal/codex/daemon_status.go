package codex

import "context"

type sessionRuntimeStatus int

const (
	sessionRuntimeUnknown sessionRuntimeStatus = iota
	sessionRuntimeWorking
	sessionRuntimeIdle
	sessionRuntimeInput
	sessionRuntimeApproval
)

type sessionStatusProvider interface {
	Fetch(context.Context, []string) (map[string]sessionRuntimeStatus, bool)
}

func parseSessionRuntimeStatus(kind string, flags []string) sessionRuntimeStatus {
	if kind == "idle" {
		return sessionRuntimeIdle
	}
	if kind != "active" {
		return sessionRuntimeUnknown
	}
	status := sessionRuntimeWorking
	for _, flag := range flags {
		switch flag {
		case "waitingOnApproval":
			return sessionRuntimeApproval
		case "waitingOnUserInput":
			status = sessionRuntimeInput
		}
	}
	return status
}
