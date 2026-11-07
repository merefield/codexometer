package codex

import "context"

type sessionRuntimeStatus int

const (
	sessionRuntimeUnknown sessionRuntimeStatus = iota
	sessionRuntimeWorking
	sessionRuntimeIdle
	sessionRuntimeInput
	sessionRuntimeApproval
	sessionRuntimeComplete
	sessionRuntimeFailed
)

type sessionStatusProvider interface {
	Fetch(context.Context, []string) (sessionDaemonSnapshot, bool)
}

type sessionDaemonSnapshot struct {
	ReadStates         map[string]SessionReadState
	Goals              map[string]*SessionGoal
	UsageCoverage      map[string]string
	Contexts           map[string]SessionContext
	WorkingDirectories map[string]string
	Statuses           map[string]sessionRuntimeStatus
	ModelObservations  []resolvedModelObservation
	SubscribedThreads  map[string]struct{}
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
	if kind == "systemError" {
		return sessionRuntimeFailed
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

// Both states describe a loaded thread with no running turn. A failed turn
// must remain recoverable without treating unknown/unloaded threads as idle.
func runtimeAcceptsFollowup(status sessionRuntimeStatus) bool {
	return status == sessionRuntimeIdle || status == sessionRuntimeFailed
}

func threadAcceptsFollowup(kind string) bool {
	return runtimeAcceptsFollowup(parseSessionRuntimeStatus(kind, nil))
}
