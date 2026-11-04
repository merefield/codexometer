package codex

import "testing"

func TestParseSessionRuntimeStatus(t *testing.T) {
	tests := []struct {
		kind  string
		flags []string
		want  sessionRuntimeStatus
	}{
		{"idle", nil, sessionRuntimeIdle},
		{"systemError", nil, sessionRuntimeFailed},
		{"active", nil, sessionRuntimeWorking},
		{"active", []string{"waitingOnUserInput"}, sessionRuntimeInput},
		{"active", []string{"waitingOnApproval"}, sessionRuntimeApproval},
		{"active", []string{"waitingOnUserInput", "waitingOnApproval"}, sessionRuntimeApproval},
		{"notLoaded", nil, sessionRuntimeUnknown},
		{"unrecognised", nil, sessionRuntimeUnknown},
	}
	for _, test := range tests {
		if got := parseSessionRuntimeStatus(test.kind, test.flags); got != test.want {
			t.Errorf("parseSessionRuntimeStatus(%q, %v) = %v; want %v", test.kind, test.flags, got, test.want)
		}
	}
}

func TestThreadAcceptsFollowup(t *testing.T) {
	for _, kind := range []string{"idle", "systemError", "active", "notLoaded", "", "failed", "unknown"} {
		want := kind == "idle" || kind == "systemError"
		if got := threadAcceptsFollowup(kind); got != want {
			t.Errorf("threadAcceptsFollowup(%q) = %v; want %v", kind, got, want)
		}
	}
}
