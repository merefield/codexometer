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
	Fetch(context.Context, []string) (sessionDaemonSnapshot, bool)
}

type sessionDaemonSnapshot struct {
	Statuses          map[string]sessionRuntimeStatus
	ModelObservations []resolvedModelObservation
	SubscribedThreads map[string]struct{}
}

// resolvedModelObservation links one exact app-server response usage event to
// the model selected by a preceding live reroute event for the same turn.
// Sequence is local to the persistent daemon connection and lets the rollout
// reader consume every observation at most once.
type resolvedModelObservation struct {
	Sequence        uint64
	ThreadID        string
	TurnID          string
	Model           string
	Usage           BenchmarkUsage
	CumulativeTotal int64
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
