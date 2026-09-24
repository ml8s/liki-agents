package domain

import (
	"errors"
	"testing"
	"time"
)

func TestLLMCallNormalize(t *testing.T) {
	call := &LLMCall{ID: " call_1 ", AgentName: " coordinator ", Model: " test-model ", ErrorCode: " runtime_timeout "}
	call.Normalize()
	if call.ID != "call_1" || call.AgentName != "coordinator" || call.Model != "test-model" || call.ErrorCode != CodeRuntimeTimeout {
		t.Fatalf("normalized call = %+v", call)
	}
}

func TestLLMCallValidate(t *testing.T) {
	started := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	valid := func() *LLMCall {
		return &LLMCall{
			ID: "call_1", RunID: "run_1", ThreadID: "thread_1", UserID: "user_1",
			AgentName: "coordinator", Model: "test-model", StartedAt: started,
		}
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	testCases := []struct {
		name     string
		mutate   func(*LLMCall)
		wantCode string
	}{
		{name: "missing id", mutate: func(c *LLMCall) { c.ID = "" }, wantCode: CodeLLMCallIDRequired},
		{name: "missing run", mutate: func(c *LLMCall) { c.RunID = "" }, wantCode: CodeRunIDRequired},
		{name: "missing thread", mutate: func(c *LLMCall) { c.ThreadID = "" }, wantCode: CodeThreadIDRequired},
		{name: "missing user", mutate: func(c *LLMCall) { c.UserID = "" }, wantCode: CodeUserIDRequired},
		{name: "missing agent", mutate: func(c *LLMCall) { c.AgentName = "" }, wantCode: CodeAgentNameRequired},
		{name: "missing model", mutate: func(c *LLMCall) { c.Model = "" }, wantCode: CodeModelRequired},
		{name: "missing start time", mutate: func(c *LLMCall) { c.StartedAt = time.Time{} }, wantCode: CodeLLMCallStartedAtRequired},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			call := valid()
			testCase.mutate(call)
			err := call.Validate()
			var domainErr *Error
			if !errors.As(err, &domainErr) || domainErr.Code != testCase.wantCode {
				t.Fatalf("Validate() error = %v, want code %s", err, testCase.wantCode)
			}
		})
	}
}

func TestLLMCallLifecycle(t *testing.T) {
	started := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	finished := started.Add(2 * time.Second)

	call := &LLMCall{ID: "call_1", StartedAt: started}
	call.Complete(LLMTokenUsage{PromptTokens: 10, CompletionTokens: 4, ThoughtTokens: 1, TotalTokens: 15}, finished)
	if call.Status != LLMCallCompleted || call.DurationMS != 2000 || call.TotalTokens != 15 {
		t.Fatalf("completed call = %+v", call)
	}

	call = &LLMCall{ID: "call_1", StartedAt: started}
	call.Fail(NewError(CodeRuntimeTimeout, "runtime timeout", nil), finished)
	if call.Status != LLMCallFailed || call.ErrorCode != CodeRuntimeTimeout || call.ErrorMessage != "runtime timeout" {
		t.Fatalf("domain failed call = %+v", call)
	}

	call = &LLMCall{ID: "call_1", StartedAt: started}
	call.Fail(errors.New("network failed"), finished)
	if call.ErrorCode != CodeLLMCallFailed || call.ErrorMessage != "LLM call failed" {
		t.Fatalf("generic failed call = %+v", call)
	}
}
