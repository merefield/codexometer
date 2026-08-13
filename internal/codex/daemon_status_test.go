package codex

import "testing"

func TestParseSessionRuntimeStatus(t *testing.T) {
	tests := []struct {
		kind  string
		flags []string
		want  sessionRuntimeStatus
	}{
		{"idle", nil, sessionRuntimeIdle},
		{"active", nil, sessionRuntimeWorking},
		{"active", []string{"waitingOnUserInput"}, sessionRuntimeInput},
		{"active", []string{"waitingOnApproval"}, sessionRuntimeApproval},
		{"active", []string{"waitingOnUserInput", "waitingOnApproval"}, sessionRuntimeApproval},
		{"notLoaded", nil, sessionRuntimeUnknown},
	}
	for _, test := range tests {
		if got := parseSessionRuntimeStatus(test.kind, test.flags); got != test.want {
			t.Errorf("parseSessionRuntimeStatus(%q, %v) = %v; want %v", test.kind, test.flags, got, test.want)
		}
	}
}
