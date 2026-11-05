package codex

// SessionAttentionLabel describes observed attention only. None is deliberately
// empty, not an inferred input request. Labels are canonical; adapters translate.
func SessionAttentionLabel(attention SessionAttention, context SessionContext) string {
	switch attention {
	case SessionAttentionInput:
		return "INPUT NEEDED"
	case SessionAttentionApproval:
		return "APPROVAL NEEDED"
	case SessionAttentionCheck:
		return "CHECK SESSION"
	case SessionAttentionError:
		return SessionFailureLabel(context)
	case SessionAttentionComplete:
		return "TURN COMPLETE"
	}
	return ""
}

// SessionStatus is the common observed-state projection. Staleness, selected
// reviews and scheduled triggers are adapter overlays, not backend attention.
func SessionStatus(active, working bool, attention SessionAttention, context SessionContext) string {
	if label := SessionAttentionLabel(attention, context); label != "" {
		return label
	}
	if working {
		return "WORKING"
	}
	if !active {
		return "INACTIVE"
	}
	return "IDLE"
}

func SessionNeedsAttention(attention SessionAttention) bool {
	return attention == SessionAttentionInput || attention == SessionAttentionApproval || attention == SessionAttentionCheck || attention == SessionAttentionError
}

func SessionRecentlyActive(active bool, attention SessionAttention) bool {
	return active && attention != SessionAttentionCheck && attention != SessionAttentionError
}
