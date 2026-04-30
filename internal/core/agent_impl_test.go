package core

import "testing"

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
