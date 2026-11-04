//go:build unix

package codex

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

func (p *daemonStatusProvider) commandGoalMenu(ctx context.Context, thread commandThread, path string) (SessionCommandMenu, error) {
	goal, err := p.readSessionGoal(ctx, thread.ID)
	menu := SessionCommandMenu{Title: "/goal", Revision: goalIdentity(goal), Choices: []SessionCommandChoice{}}
	if err != nil {
		return menu, fmt.Errorf("goal state unavailable; check shared app-server goal support: %w", err)
	}
	summary := goalCommandSummary(goal)
	if goal != nil && path != "goal" {
		budget := "unlimited"
		if goal.TokenBudget != nil {
			budget = strconv.FormatInt(*goal.TokenBudget, 10)
		}
		// Progress is live telemetry, not a different requested mutation. Keep
		// action help/IDs stable while only token/time counters advance.
		summary = fmt.Sprintf("%s\nObjective: %s\nTotal token budget: %s", goal.Label(), SanitizeSessionContext(goal.Objective), budget)
	}
	menu.Help = summary + "\nGoal changes affect only this session. Creating, editing or resuming an active goal can start work and consume quota. No automatic retries."
	if goal != nil && (!knownGoalStatus(goal.Status) || goal.Stale) {
		return menu, errors.New("current goal state is unknown; refresh before changing it")
	}
	if path == "goal" {
		if goal != nil {
			menu.Choices = append(menu.Choices, SessionCommandChoice{Label: goal.Label(), Help: goalCommandSummary(goal)})
		}
		menu.Choices = append(menu.Choices, SessionCommandChoice{Label: "Set goal", Help: "Set an objective and activate pursuit. May start an idle turn. Existing token budget is retained; use Budget to change it.", Next: "goal/set"})
		if goal != nil {
			menu.Choices = append(menu.Choices,
				SessionCommandChoice{Label: "Edit goal", Help: "Edit the objective. Paused/stalled states are retained; editing a completed or budget-limited goal reactivates it.", Next: "goal/edit"},
				SessionCommandChoice{Label: "Token budget", Help: "Set a total goal-token budget, or unlimited. This is not an account quota limit.", Next: "goal/budget"})
			if goal.Status == "active" {
				menu.Choices = append(menu.Choices, SessionCommandChoice{Label: "Pause goal", Help: "Pause continued pursuit; does not interrupt a running turn.", Next: "goal/pause"})
			} else {
				menu.Choices = append(menu.Choices, SessionCommandChoice{Label: "Resume goal", Help: "Resume pursuit. May start work; server budget and usage limits still apply.", Next: "goal/resume"})
			}
			menu.Choices = append(menu.Choices, SessionCommandChoice{Label: "Clear goal", Help: "Remove this session's goal. Does not delete the session or interrupt its current turn.", Next: "goal/clear"})
		}
		return menu, nil
	}
	parent, value, reviewing := SessionCommandInputValue(path)
	if !reviewing {
		parent = path
	}
	if parent == "goal/set" || parent == "goal/edit" || parent == "goal/budget" {
		if parent != "goal/set" && goal == nil {
			return menu, errors.New("no goal is set for this session")
		}
		if !reviewing {
			menu.Input, menu.InputLabel, menu.InputLimit = true, "Goal objective", SessionGoalInputLimit
			if parent == "goal/edit" {
				menu.Value = goal.Objective
			} else if parent == "goal/budget" {
				menu.InputLabel, menu.InputLimit, menu.Value = "Token budget (or unlimited)", 32, "unlimited"
				if goal.TokenBudget != nil {
					menu.Value = strconv.FormatInt(*goal.TokenBudget, 10)
				}
			}
			return menu, nil
		}
		params := map[string]any{"threadId": thread.ID, "origin": "user"}
		label := "Apply goal objective"
		if parent == "goal/budget" {
			label = "Apply goal token budget"
			if strings.EqualFold(value, "unlimited") {
				params["tokenBudget"] = nil
			} else {
				budget, err := strconv.ParseInt(value, 10, 64)
				if err != nil || budget <= 0 || budget > 9007199254740991 {
					return menu, errors.New("budget must be a positive integer up to 9007199254740991, or unlimited")
				}
				params["tokenBudget"] = budget
			}
			menu.Help += "\nNew total token budget: " + value + ". A stopped goal is not automatically resumed."
		} else {
			if !utf8.ValidString(value) || utf8.RuneCountInString(value) > SessionGoalInputLimit || strings.TrimSpace(value) == "" || SanitizeSessionContext(value) != value {
				return menu, errors.New("goal objective must be safe non-empty text within the input limit")
			}
			params["objective"] = value
			status := "active"
			if parent == "goal/edit" && goal.Status != "complete" && goal.Status != "budgetLimited" {
				status = goal.Status
			}
			params["status"] = status
			menu.Help += "\nRequested objective: " + value + "\nRequested status: " + status + ". Existing token budget is retained."
		}
		menu.Choices = append(menu.Choices, SessionCommandChoice{Label: label, Help: menu.Help, method: "thread/goal/set", params: params})
		return menu, nil
	}
	if goal == nil {
		return menu, errors.New("no goal is set for this session")
	}
	params := map[string]any{"threadId": thread.ID, "origin": "user"}
	method, label := "thread/goal/set", ""
	switch path {
	case "goal/pause":
		label, params["status"] = "Pause goal", "paused"
		menu.Help += "\nPausing stops continued pursuit, not an already-running turn."
	case "goal/resume":
		label, params["status"] = "Resume goal", "active"
		menu.Help += "\nResuming may immediately start further turns and consume quota. Budget and account limits still apply."
	case "goal/clear":
		label, method = "Clear goal", "thread/goal/clear"
		menu.Help += "\nThis removes the goal, not the conversation. A running turn is not interrupted."
	default:
		return menu, ErrSessionCommand
	}
	menu.Choices = append(menu.Choices, SessionCommandChoice{Label: label, Help: menu.Help, method: method, params: params})
	return menu, nil
}

func (p *daemonStatusProvider) executeGoalCommand(ctx context.Context, conn *websocket.Conn, id string, choice SessionCommandChoice) error {
	p.mu.Lock()
	_, subscribed := p.subscribed[id]
	current := p.connection == conn
	p.mu.Unlock()
	if !current || !subscribed {
		return ErrSessionCommand
	}
	var result struct {
		Goal    *SessionGoal
		Cleared *bool
	}
	if err := p.requestOn(ctx, conn, choice.method, choice.params, &result); err != nil {
		return err // Never retry a possibly applied goal mutation.
	}
	if choice.method == "thread/goal/clear" {
		if result.Cleared == nil {
			return errors.New("goal clear acknowledgement unavailable; check Codex")
		}
	} else {
		if result.Goal == nil || validateSessionGoal(result.Goal, id) != nil {
			return errors.New("goal change acknowledgement unavailable; check Codex")
		}
		if status, ok := choice.params["status"].(string); ok && result.Goal.Status != status {
			return errors.New("goal status acknowledgement differs; check Codex")
		}
	}
	goal, err := p.readSessionGoal(ctx, id)
	if err != nil {
		return errors.New("goal change unconfirmed; check Codex before retrying")
	}
	if choice.method == "thread/goal/clear" {
		if goal != nil {
			return errors.New("goal clear unconfirmed; check Codex")
		}
		return nil
	}
	if goal == nil {
		return errors.New("goal change unconfirmed; check Codex")
	}
	if objective, ok := choice.params["objective"].(string); ok && goal.Objective != objective {
		return errors.New("goal objective update unconfirmed; check Codex")
	}
	if budget, changing := choice.params["tokenBudget"]; changing {
		if budget == nil && goal.TokenBudget != nil || budget != nil && (goal.TokenBudget == nil || *goal.TokenBudget != budget.(int64)) {
			return errors.New("goal budget update unconfirmed; check Codex")
		}
	}
	// Active pursuit can legitimately become blocked/limited/complete after
	// the acknowledged update but before the verification read.
	if desired, ok := choice.params["status"].(string); ok && desired != "active" && goal.Status != desired {
		return errors.New("goal status update unconfirmed; check Codex")
	}
	return nil
}
