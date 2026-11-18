//go:build unix

package codex

import (
	"context"
	"errors"
	"time"

	"github.com/gorilla/websocket"
)

type pendingModelChange struct {
	connection     *websocket.Conn
	before, target QuotaSession
	params         map[string]any
	display        PendingModelChange
	applying       bool
	writing        bool
}

// Discovery and settings reads may span another client's turn start. Decide
// from fresh state before superseding an older intent, under settingsMu.
func (p *daemonStatusProvider) setOrQueueModel(ctx context.Context, conn *websocket.Conn, before, target QuotaSession, params map[string]any) error {
	err := p.requireIdleModelThread(ctx, before.ID)
	if err != nil && !errors.Is(err, errModelStillWorking) {
		return err
	}
	p.mu.Lock()
	if p.connection != conn {
		p.mu.Unlock()
		return ErrSessionCommand
	}
	queue := err != nil || p.commandHumanWaitLocked(before.ID)
	if !queue {
		delete(p.pendingModels, before.ID)
	}
	p.mu.Unlock()
	if queue {
		return p.queueModelChange(conn, before, target, params)
	}
	return p.writeQuotaSettingsOn(ctx, conn, target, params)
}

// Called with settingsMu held. One pending change per session: an explicit
// later selection replaces an unsent selection, never an in-flight write.
func (p *daemonStatusProvider) queueModelChange(conn *websocket.Conn, before, target QuotaSession, params map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if conn == nil || conn != p.connection {
		return ErrSessionCommand
	}
	if existing := p.pendingModels[before.ID]; existing != nil && existing.writing {
		return errors.New("model change already sending; wait for the outcome")
	}
	if len(p.pendingModels) >= 32 && p.pendingModels[before.ID] == nil {
		return errors.New("too many pending model changes")
	}
	if p.pendingModels == nil {
		p.pendingModels = make(map[string]*pendingModelChange)
	}
	p.pendingModels[before.ID] = &pendingModelChange{connection: conn, before: before, target: target, params: params,
		display: PendingModelChange{Summary: target.Model + " // " + target.Effort + " // DEFAULT", State: "queued"}}
	// The stop notification can have arrived just before the queue was stored.
	go p.flushModelChanges(before.ID)
	return nil
}

// Native turn-stop events wake pending work; ordinary telemetry polling covers
// missed events. A fresh thread/read must still prove idle before any write.
func (p *daemonStatusProvider) flushModelChanges(wake ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, item := range p.pendingModels {
		if item.applying || item.display.State != "queued" {
			continue
		}
		state := p.statuses[id]
		if (state == sessionRuntimeWorking || state == sessionRuntimeApproval || state == sessionRuntimeInput) && (len(wake) == 0 || wake[0] != id) {
			continue // Do not add a thread/read on every UI tick while still working.
		}
		item.applying = true
		go p.applyModelChange(id, item)
	}
}

func (p *daemonStatusProvider) applyModelChange(id string, item *pendingModelChange) {
	p.settingsMu.Lock()
	defer p.settingsMu.Unlock()
	p.mu.Lock()
	valid := !p.settingsClosed && p.connection == item.connection && p.pendingModels[id] == item
	p.mu.Unlock()
	if !valid {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	err := p.applyIdleModelChange(ctx, id, item)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.connection != item.connection || p.pendingModels[id] != item {
		return
	}
	item.applying = false
	item.writing = false
	switch {
	case errors.Is(err, errModelStillWorking):
		// No mutation was attempted. A later stop/poll may try the idle check.
	case err == nil:
		delete(p.pendingModels, id)
	default:
		item.display.State = "failed; reselect /model"
		if errors.Is(err, ErrQuotaProfileUncertain) || errors.Is(err, ErrQuotaProfileUnverified) {
			item.display.State = "unconfirmed; check Codex before retrying"
		}
	}
}

var errModelStillWorking = errors.New("model change waiting for idle")

func (p *daemonStatusProvider) applyIdleModelChange(ctx context.Context, id string, item *pendingModelChange) error {
	if err := p.requireIdleModelThread(ctx, id); err != nil {
		return err
	}
	if _, err := p.checkQuotaSettings(ctx, item.before); err != nil {
		return err // Do not overwrite another client's model, effort or speed.
	}
	if _, err := p.validateQuotaStep(ctx, QuotaStep{Model: item.target.Model, Effort: item.target.Effort, ServiceTier: "default"}, item.before.ModelProvider); err != nil {
		return err
	}
	if _, err := p.checkQuotaSettings(ctx, item.before); err != nil {
		return err
	}
	// Discovery can span a turn transition. Do not write into the new turn.
	if err := p.requireIdleModelThread(ctx, id); err != nil {
		return err
	}
	p.mu.Lock()
	valid := p.connection == item.connection && p.pendingModels[id] == item
	waiting := p.commandHumanWaitLocked(id)
	if valid && !waiting {
		item.writing = true
	}
	p.mu.Unlock()
	if !valid {
		return ErrSessionCommand
	}
	if waiting {
		return errModelStillWorking // Let the fresh telemetry poll clear stale wait flags.
	}
	return p.writeQuotaSettingsOn(ctx, item.connection, item.target, item.params)
}

func (p *daemonStatusProvider) requireIdleModelThread(ctx context.Context, id string) error {
	t, err := p.commandThread(ctx, id)
	if err != nil {
		return err
	}
	if threadAcceptsFollowup(t.Status.Type) {
		return nil
	}
	if t.Status.Type == "active" {
		return errModelStillWorking
	}
	return ErrSessionCommand // Unknown/not-loaded states are not a new running turn.
}
