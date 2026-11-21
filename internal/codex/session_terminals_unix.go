//go:build unix

package codex

import "context"

func (p *daemonStatusProvider) InspectSessionTerminals(ctx context.Context, thread string) (TerminalInspection, error) {
	p.mu.Lock()
	conn, generation := p.connection, p.commandGeneration
	_, subscribed := p.subscribed[thread]
	p.mu.Unlock()
	if conn == nil || !subscribed {
		return TerminalInspection{}, ErrSessionTerminals
	}
	const limit = 100
	var reply struct {
		NextCursor string
		Data       []struct {
			ProcessID, Command, Cwd string
			OSPID                   uint32 `json:"osPid"`
		}
	}
	if err := p.requestOn(ctx, conn, "thread/backgroundTerminals/list", map[string]any{"threadId": thread, "limit": limit}, &reply); err != nil {
		return TerminalInspection{}, ErrSessionTerminals
	}
	p.mu.Lock()
	_, subscribed = p.subscribed[thread]
	valid := subscribed && p.connection == conn && p.commandGeneration == generation
	p.mu.Unlock()
	if !valid {
		return TerminalInspection{}, ErrSessionTerminals
	}
	out := TerminalInspection{Terminals: []SessionTerminal{}, More: reply.NextCursor != "" || len(reply.Data) > limit}
	for _, item := range reply.Data[:min(len(reply.Data), limit)] {
		out.Terminals = append(out.Terminals, SessionTerminal{singleLine(item.ProcessID), SanitizeSessionContext(item.Command), singleLine(item.Cwd), item.OSPID})
	}
	return out, nil
}
