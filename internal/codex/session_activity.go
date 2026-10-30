package codex

import "time"

// SessionActivity keeps prose separate from shell activity. It never contains
// approval capabilities or command output; all display strings are bounded.
type SessionActivity struct {
	Prose           string
	Command         string
	CommandStatus   string
	RunningCommands int
	RunningLimited  bool
	// Server-recorded item times, not observer arrival times. Missing means
	// unknown; they neither refresh session freshness nor affect token accounting.
	CommandStartedAt   time.Time
	CommandCompletedAt time.Time
}

// CommandTiming reports elapsed or completed duration only with valid timing
// evidence. Running timers use the caller's snapshot time, so stale web data
// does not keep pretending that the command is still running.
func (a SessionActivity) CommandTiming(now time.Time) string {
	if a.CommandStartedAt.IsZero() || a.CommandStartedAt.After(now) {
		return ""
	}
	end := a.CommandCompletedAt
	if a.CommandStatus == "running" {
		end = now
	}
	if end.IsZero() || end.Before(a.CommandStartedAt) || end.After(now) {
		return ""
	}
	return end.Sub(a.CommandStartedAt).Truncate(time.Second).String()
}

type activityCommand struct {
	text               string
	status             string
	sequence           uint64
	started, completed time.Time
}

type sessionActivityState struct {
	prose       string
	proseTurnID string
	last        activityCommand
	running     map[string]activityCommand
	sequence    uint64
	limited     bool
}

func (s *sessionActivityState) command(id, text, status string, finished bool) {
	s.commandTimed(id, text, status, finished, time.Time{}, time.Time{})
}

func (s *sessionActivityState) commandTimed(id, text, status string, finished bool, started, completed time.Time) {
	previous := s.running[id]
	text = SanitizeSessionContext(text)
	if text == "" {
		text = s.running[id].text
	}
	if text == "" {
		return
	}
	s.sequence++
	if finished {
		delete(s.running, id)
		switch status {
		case "completed", "failed", "declined":
		default:
			status = "unknown"
		}
	} else {
		if status == "inProgress" {
			status = "running"
		} else {
			status = "unknown"
		}
	}
	if started.IsZero() {
		started = previous.started
	}
	if !finished {
		completed = time.Time{}
	}
	if !completed.IsZero() && !started.IsZero() && completed.Before(started) {
		completed = time.Time{}
	}
	c := activityCommand{text: text, status: status, sequence: s.sequence, started: started, completed: completed}
	s.last = c
	if !finished && id != "" && status == "running" {
		if s.running == nil {
			s.running = make(map[string]activityCommand)
		}
		if _, exists := s.running[id]; exists || len(s.running) < 64 {
			s.running[id] = c
		} else {
			s.limited = true
		}
	}
}

func (s sessionActivityState) snapshot() SessionActivity {
	command := s.last
	// Prefer the most recently observed still-running command over a command
	// that finished while another parallel command continues.
	var sequence uint64
	for _, c := range s.running {
		if c.sequence > sequence {
			command, sequence = c, c.sequence
		}
	}
	return SessionActivity{Prose: s.prose, Command: command.text, CommandStatus: command.status,
		RunningCommands: len(s.running), RunningLimited: s.limited,
		CommandStartedAt: command.started, CommandCompletedAt: command.completed}
}

func (s sessionActivityState) context(c SessionContext) SessionContext {
	c.Activity = s.snapshot()
	c.Kind = SessionContextActivity
	c.Text = c.Activity.Prose
	if c.Text == "" {
		c.Text = c.Activity.Command
	}
	return c
}
