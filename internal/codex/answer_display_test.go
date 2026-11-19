package codex

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAnswerDisplayContract(t *testing.T) {
	for _, tc := range []struct {
		raw      string
		restrict bool
	}{
		{"", false}, {"null", false}, {`{"moderated":false}`, false},
		{`{"moderated":true,"display":{"version":1,"mode":"show","trustedContacts":"skip","extra":42}}`, false},
		{`{"moderated":true,"display":null}`, true},
		{`{"moderated":false,"display":{"version":1,"mode":"hide","replacement":{"kind":"wellbeingPromptHelpline"}}}`, true},
		{`{"moderated":true,"display":{"version":1,"mode":"block","reason":{"kind":"protection","protectionType":"fixture"}}}`, true},
		{`{"moderated":true,"display":{"version":1,"mode":"show","trustedContacts":"check"}}`, true},
		{`{"moderated":true,"display":{"version":1,"mode":"show","trustedContacts":"skip","annotation":{"kind":"wellbeingHelpline","subject":"answer"}}}`, true},
		{`{"moderated":false,"display":{"version":2,"mode":"show","trustedContacts":"skip"}}`, true},
		{`{"moderated":false,"display":{"action":"legacy"}}`, true},
		{`{"moderated":true,"display":{"version":1,"mode":"show"}}`, true},
		{`{"display":null}`, true}, {`"malformed"`, true},
	} {
		if got := restrictedAnswer(json.RawMessage(tc.raw)); got != tc.restrict {
			t.Errorf("%s: restricted = %t, want %t", tc.raw, got, tc.restrict)
		}
	}
}

func TestRestrictedLiveAnswerStopsStreamingAndCannotReplay(t *testing.T) {
	states := map[string]*daemonContextState{}
	emit := func(method, raw string) {
		daemonContextEvent(states, method, nil, json.RawMessage(raw), time.Now())
	}
	emit("turn/started", `{"threadId":"s","turn":{"id":"t"}}`)
	item := `{"threadId":"s","turnId":"t","item":{"id":"a","type":"agentMessage","phase":"final_answer","text":"fixture plaintext"%s}}`
	plain := strings.Replace(item, "%s", "", 1)
	blocked := strings.Replace(item, "%s", `,"moderation":{"moderated":true}`, 1)
	delta := `{"threadId":"s","turnId":"t","itemId":"a","delta":"partial"}`
	emit("item/started", plain)
	emit("item/agentMessage/delta", delta)
	if !states["s"].latest.Streaming {
		t.Fatal("ordinary stream was buffered")
	}
	for _, e := range [][2]string{{"item/completed", blocked}, {"item/started", blocked}, {"item/agentMessage/delta", delta}, {"item/completed", plain}} {
		emit(e[0], e[1])
		got := states["s"].latest
		if !got.ReplyRestricted || got.Text != "" || got.Streaming || states["s"].streamText != "" {
			t.Fatalf("%s retained restricted text: %+v", e[0], got)
		}
	}
	emit("item/completed", strings.Replace(plain, `"id":"a"`, `"id":"b"`, 1))
	if got := states["s"].latest; got.ReplyRestricted || got.Text != "fixture plaintext" {
		t.Fatal("restriction was applied to an unrelated item", got)
	}
}

func TestRestrictedRolloutMirrorsAndRestart(t *testing.T) {
	for _, decision := range []string{
		`{"type":"agent_message","phase":"final_answer","message":"fixture plaintext","moderation":{"item_id":"a","moderated":true}}`,
		`{"type":"item_completed","turn_id":"t","item":{"type":"AgentMessage","id":"a","content":[{"type":"Text","text":"fixture plaintext"}],"moderation":{"moderated":true}}}`,
	} {
		now, home := time.Now(), t.TempDir()
		path := testRolloutPath(t, home, now, "s")
		event := func(p string) string { return `{"type":"event_msg","payload":` + p + "}\n" }
		writeRollout(t, path, sessionMetaLine("s", `"cli"`, "/example", nil)+"\n"+event(`{"type":"task_started","turn_id":"t"}`)+event(decision)+
			`{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"type":"output_text","text":"fixture plaintext"}]}}`+"\n"+
			event(`{"type":"task_complete","turn_id":"t","last_agent_message":"fixture plaintext"}`))
		for _, compressed := range []bool{false, true} {
			if compressed {
				compressTestRollout(t, path, true)
			}
			r, err := NewLiveUsageReader(home)
			if err != nil {
				t.Fatal(err)
			}
			r.statusProvider = nil
			u, err := r.FetchTokenUsage(context.Background())
			if err != nil || len(u.Sessions) != 1 {
				t.Fatal(u, err)
			}
			c := u.Sessions[0].Context
			if c.Text != "" || !c.ReplyRestricted || c.TurnID != "t" {
				t.Fatal("restart exposed mirrored text", c)
			}
			for _, cursor := range r.files {
				cursor.currentTurnID = "next"
				c, ok := rolloutContextRecord([]byte(event(`{"type":"task_complete","turn_id":"next","last_agent_message":"safe"}`)), cursor)
				if !ok || c.Text != "safe" || c.ReplyRestricted {
					t.Fatal("unrelated turn hidden", c)
				}
			}
		}
	}
}

func TestTurnSummaryRestrictsAlreadyStreamedAnswer(t *testing.T) {
	states := map[string]*daemonContextState{}
	for _, e := range [][2]string{
		{"item/started", `{"threadId":"s","turnId":"t","item":{"type":"agentMessage","phase":"final_answer","id":"a"}}`},
		{"item/agentMessage/delta", `{"threadId":"s","turnId":"t","itemId":"a","delta":"fixture plaintext"}`},
		{"turn/completed", `{"threadId":"s","turn":{"id":"t","status":"completed","items":[{"type":"agentMessage","id":"a","text":"fixture plaintext","moderation":{"moderated":true}}]}}`},
	} {
		daemonContextEvent(states, e[0], nil, json.RawMessage(e[1]), time.Now())
	}
	c := daemonContextSnapshot(states, []string{"s"}, nil)["s"]
	if !c.ReplyRestricted || c.Text != "" || c.Streaming || states["s"].streamText != "" {
		t.Fatal(c)
	}
}

func TestTruncatedTailDoesNotPublishOrphanedAnswerMirror(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	final := `{"type":"event_msg","payload":{"type":"task_complete","turn_id":"t","last_agent_message":"unverified mirror"}}` + "\n"
	writeRollout(t, path, strings.Repeat("x", 256*1024)+"\n"+final)
	cursor := &rolloutCursor{threadID: "s", currentTurnID: "t"}
	if c := latestSessionContext(path, cursor); c.Text != "" {
		t.Fatal(c)
	}
	c, ok := rolloutContextRecord([]byte(final), cursor)
	if !ok || c.Text != "" {
		t.Fatal("subsequent mirror exposed truncated history", c)
	}
	rolloutContextRecord([]byte(`{"type":"event_msg","payload":{"type":"task_started","turn_id":"next"}}`), cursor)
	cursor.currentTurnID = "next"
	if c, ok := rolloutContextRecord([]byte(strings.ReplaceAll(final, `"t"`, `"next"`)), cursor); !ok || c.Text != "unverified mirror" {
		t.Fatal("new turn lost ordinary reply", c)
	}
}
