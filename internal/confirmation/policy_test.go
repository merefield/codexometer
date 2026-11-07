package confirmation

import "testing"

func TestPolicy(t *testing.T) {
	for _, mode := range []Mode{"", Balanced, None, Require} {
		for _, action := range []Action{Guarded, Narrow, QuotaReset, Action(99)} {
			want := action == QuotaReset || mode != None && (mode == Require || action != Narrow)
			if (Policy{Mode: mode}).Required(action) != want {
				t.Fatal("wrong confirmation policy", mode, action)
			}
		}
	}
	if Command(false) != Guarded || Command(true) != Narrow {
		t.Fatal("unknown commands must remain guarded")
	}
	for _, kind := range []string{"accept", "decline", "cancel", "acceptForSession", "acceptWithExecpolicyAmendment", "acceptMcpAlways", "cancelMcpTool", "allowNetworkHost", "denyNetworkHost", "unknown"} {
		narrow := kind == "accept" || kind == "decline" || kind == "cancel" || kind == "cancelMcpTool"
		if (Approval(kind) == Narrow) != narrow {
			t.Fatal("incorrect approval scope", kind)
		}
	}
}

func TestParseMode(t *testing.T) {
	for _, value := range []string{"balanced", "none", "require", "", "no", "true", "never"} {
		mode, err := Parse(value)
		valid := value == "balanced" || value == "none" || value == "require"
		if (err == nil) != valid || valid && string(mode) != value {
			t.Fatal(value, mode, err)
		}
	}
}
