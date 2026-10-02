package codex

// SessionActivity keeps prose separate from shell activity. It never contains
// approval capabilities or command output; all display strings are bounded.
type SessionActivity struct {
	Prose           string
	Command         string
	CommandStatus   string
	RunningCommands int
	RunningLimited  bool
}

type activityCommand struct {
	text     string
	status   string
	sequence uint64
}

type sessionActivityState struct {
	prose    string
	last     activityCommand
	running  map[string]activityCommand
	sequence uint64
	limited  bool
}

func (s *sessionActivityState) command(id, text, status string, finished bool) {
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
	c := activityCommand{text: text, status: status, sequence: s.sequence}
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
		RunningCommands: len(s.running), RunningLimited: s.limited}
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
