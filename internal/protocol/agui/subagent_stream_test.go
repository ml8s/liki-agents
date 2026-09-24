package agui_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ml8s/liki-agents/internal/agent"
	"github.com/ml8s/liki-agents/internal/protocol/agui"
	"google.golang.org/adk/v2/session"
)

type subagentEvent struct {
	Type          string `json:"type"`
	SubagentRunID string `json:"subagentRunId,omitempty"`
	Name          string `json:"name,omitempty"`
	ParentRunID   string `json:"parentSubagentRunId,omitempty"`
	Message       string `json:"message,omitempty"`
	Code          string `json:"code,omitempty"`
	Delta         string `json:"delta,omitempty"`
}

func sseEvents(t *testing.T, body string) []subagentEvent {
	t.Helper()
	var events []subagentEvent
	for _, line := range strings.Split(body, "\n") {
		encoded, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var event subagentEvent
		if err := json.Unmarshal([]byte(encoded), &event); err != nil {
			t.Fatalf("decode SSE event %q: %v", encoded, err)
		}
		events = append(events, event)
	}
	return events
}

func TestAGUIEmitsOfficialSubagentLifecycle(t *testing.T) {
	events := []*session.Event{
		{Author: "planner", Branch: "main.planner"},
		{Author: "worker", Branch: "main.planner.worker"},
		{Author: "worker", Branch: "main.planner.worker"},
		{Author: "planner", Branch: "main.planner"},
		{Author: "main", Branch: "main"},
	}
	scripted := &scriptedRuntime{
		events: events,
		result: agent.RunResult{Text: "complete"},
	}
	handler, err := agui.New(scripted, agui.Config{
		RunTimeout: time.Second,
		Entrypoint: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := authenticatedRequest(t, `{"threadId":"t","runId":"r","messages":[{"id":"m","role":"user","content":"hi"}]}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	events2 := sseEvents(t, response.Body.String())
	var kinds []string
	for _, event := range events2 {
		kinds = append(kinds, event.Type)
	}
	want := []string{
		"RUN_STARTED",
		"SUBAGENT_STARTED",  // planner
		"SUBAGENT_STARTED",  // worker
		"SUBAGENT_FINISHED", // worker
		"SUBAGENT_FINISHED", // planner
		"TEXT_MESSAGE_START",
		"TEXT_MESSAGE_CONTENT",
		"TEXT_MESSAGE_END",
		"RUN_FINISHED",
	}
	if len(kinds) < len(want) {
		t.Fatalf("events = %#v, want at least %#v", kinds, want)
	}
	for index, kind := range want {
		if kinds[index] != kind {
			t.Fatalf("event %d = %q, want %q; all=%#v", index, kinds[index], kind, kinds)
		}
	}

	started := make([]subagentEvent, 0, 2)
	finished := make([]subagentEvent, 0, 2)
	for _, event := range events2 {
		switch event.Type {
		case "SUBAGENT_STARTED":
			started = append(started, event)
		case "SUBAGENT_FINISHED":
			finished = append(finished, event)
		}
	}
	if len(started) != 2 || len(finished) != 2 {
		t.Fatalf("subagent events = %d started / %d finished", len(started), len(finished))
	}
	if started[0].Name != "planner" || started[1].Name != "worker" {
		t.Fatalf("started names = %#v", started)
	}
	if started[1].ParentRunID != started[0].SubagentRunID {
		t.Fatalf("worker parent = %q, want planner %q", started[1].ParentRunID, started[0].SubagentRunID)
	}
	if finished[0].SubagentRunID != started[1].SubagentRunID ||
		finished[1].SubagentRunID != started[0].SubagentRunID {
		t.Fatalf("finished order = %#v / %#v", started, finished)
	}
}

func TestAGUIEmitsSubagentErrorBeforeRunError(t *testing.T) {
	scripted := &scriptedRuntime{
		events: []*session.Event{{Author: "worker", Branch: "main.worker"}},
		err:    context.DeadlineExceeded,
	}
	handler, err := agui.New(scripted, agui.Config{
		RunTimeout: time.Second,
		Entrypoint: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	request := authenticatedRequest(t, `{"threadId":"t","runId":"r","messages":[{"id":"m","role":"user","content":"hi"}]}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	events := sseEvents(t, response.Body.String())
	subagentError := -1
	runError := -1
	for index, event := range events {
		switch event.Type {
		case "SUBAGENT_ERROR":
			subagentError = index
			if event.Code != "runtime_timeout" {
				t.Fatalf("subagent error code = %q", event.Code)
			}
		case "RUN_ERROR":
			runError = index
			if event.Code != "runtime_timeout" {
				t.Fatalf("run error code = %q", event.Code)
			}
		}
	}
	if subagentError < 0 || runError < 0 || subagentError > runError {
		t.Fatalf("events=%#v subagentError=%d runError=%d", events, subagentError, runError)
	}
}
