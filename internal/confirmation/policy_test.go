package confirmation

import "testing"

func TestPolicy(t *testing.T) {
	for _, tc := range []struct {
		noConfirm bool
		action    Action
		want      bool
	}{
		{false, Guarded, true}, {false, Reversible, false},
		{false, QuotaReset, true}, {false, Action(99), true},
		{true, Guarded, false}, {true, Reversible, false},
		{true, QuotaReset, true}, {true, Action(99), false},
	} {
		if (Policy{NoConfirm: tc.noConfirm}).Required(tc.action) != tc.want {
			t.Fatal("wrong confirmation policy", tc)
		}
	}
	if Command(false) != Guarded || Command(true) != Reversible {
		t.Fatal("unknown command must remain guarded")
	}
}
