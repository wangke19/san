package core

import (
	"context"
	"testing"
	"time"
)

func TestEstimatePromptTokensUsesConversationGrowth(t *testing.T) {
	got := estimatePromptTokens(1000, 2000, 3000)
	if got != 1500 {
		t.Fatalf("estimatePromptTokens() = %d, want 1500", got)
	}
}

func TestEstimatePromptTokensNeverDropsBelowLastKnownPromptSize(t *testing.T) {
	got := estimatePromptTokens(1000, 3000, 2000)
	if got != 1000 {
		t.Fatalf("estimatePromptTokens() = %d, want 1000", got)
	}
}

// blockingLLM blocks Infer until the caller signals release. Used to hold a
// turn open so InterruptCurrentTurn has something live to cancel.
type blockingLLM struct {
	release chan struct{}
}

func (b *blockingLLM) InputLimit() int { return 0 }

func (b *blockingLLM) Infer(ctx context.Context, _ InferRequest) (<-chan Chunk, error) {
	ch := make(chan Chunk, 1)
	go func() {
		defer close(ch)
		select {
		case <-ctx.Done():
			ch <- Chunk{Err: ctx.Err()}
		case <-b.release:
			ch <- Chunk{
				Done: true,
				Response: &InferResponse{
					Content:    "released",
					StopReason: StopEndTurn,
				},
			}
		}
	}()
	return ch, nil
}

func TestInterruptCurrentTurnReturnsToWaitInsteadOfEndingRun(t *testing.T) {
	llm := &blockingLLM{release: make(chan struct{})}
	ag := NewAgent(Config{
		ID:     "test",
		LLM:    llm,
		System: NewSystem(),
		Tools:  NewTools(),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runDone := make(chan error, 1)
	go func() { runDone <- ag.Run(ctx) }()

	// Drain outbox in the background so emit calls don't block.
	go func() {
		for range ag.Outbox() {
		}
	}()

	// Kick off the first turn, then interrupt while Infer is blocked.
	ag.Inbox() <- Message{Role: RoleUser, Content: "first"}
	waitFor(t, "blockingLLM call to start", func() bool {
		select {
		case <-llm.release:
			return false
		default:
		}
		// turnCancel is stored only after ThinkAct enters streamInfer.
		return ag.(*agent).turnCancel.Load() != nil
	})

	ag.InterruptCurrentTurn()

	// Run must still be alive after the interrupt — verify by sending a
	// second message and letting the LLM finish. If Run had exited, this
	// send would block forever (inbox has no reader).
	select {
	case err := <-runDone:
		t.Fatalf("Run returned after interrupt, expected it to keep running; err=%v", err)
	case <-time.After(100 * time.Millisecond):
	}

	ag.Inbox() <- Message{Role: RoleUser, Content: "second"}
	close(llm.release)

	// Now shut down cleanly.
	ag.Inbox() <- Message{Signal: SigStop}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned error on shutdown: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after SigStop")
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", what)
}

func TestCanExecuteToolBatchInParallelOnlyAllowsReadOnlyTools(t *testing.T) {
	tests := []struct {
		name  string
		tasks []agentToolTask
		want  bool
	}{
		{
			name: "all read only",
			tasks: []agentToolTask{
				{call: ToolCall{Name: "Read"}},
				{call: ToolCall{Name: "Grep"}},
				{call: ToolCall{Name: "Glob"}},
			},
			want: true,
		},
		{
			name: "edit serializes batch",
			tasks: []agentToolTask{
				{call: ToolCall{Name: "Read"}},
				{call: ToolCall{Name: "Edit"}},
			},
			want: false,
		},
		{
			name: "bash serializes batch",
			tasks: []agentToolTask{
				{call: ToolCall{Name: "Bash"}},
				{call: ToolCall{Name: "Read"}},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canExecuteToolBatchInParallel(tt.tasks); got != tt.want {
				t.Fatalf("canExecuteToolBatchInParallel() = %v, want %v", got, tt.want)
			}
		})
	}
}
