package intelligentrouter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"gomodel/internal/core"
)

// fakeExecutor is a ChatCompletionExecutor that returns canned responses per
// analyzer model, allowing failover behavior to be exercised.
type fakeExecutor struct {
	responses    map[string]string   // model -> content (legacy single response)
	responseSeq  map[string][]string // model -> ordered contents per call
	errs         map[string]error    // model -> forced error (legacy single error)
	errsSeq      map[string][]error  // model -> ordered errors per call
	calls        []string            // ordered list of models called
}

func (f *fakeExecutor) ChatCompletion(_ context.Context, req *core.ChatRequest) (*core.ChatResponse, error) {
	f.calls = append(f.calls, req.Model)
	if seq, ok := f.errsSeq[req.Model]; ok && len(seq) > 0 {
		err := seq[0]
		f.errsSeq[req.Model] = seq[1:]
		if err != nil {
			return nil, err
		}
	}
	if err, ok := f.errs[req.Model]; ok {
		return nil, err
	}
	if seq, ok := f.responseSeq[req.Model]; ok && len(seq) > 0 {
		content := seq[0]
		f.responseSeq[req.Model] = seq[1:]
		return &core.ChatResponse{
			Choices: []core.Choice{{Message: core.ResponseMessage{Content: content}, FinishReason: "stop"}},
		}, nil
	}
	content, ok := f.responses[req.Model]
	if !ok {
		return nil, errors.New("no canned response for " + req.Model)
	}
	return &core.ChatResponse{
		Choices: []core.Choice{{Message: core.ResponseMessage{Content: content}, FinishReason: "stop"}},
	}, nil
}

const validClassification = `{"complexity":"low","task_type":"chat","requires_reasoning":false,"requires_code":false,"requires_long_context":false,"requires_vision":false,"requires_tools":false,"quality_sensitivity":"low","suggested_tier":"cheap","confidence":0.9,"reason":"simple greeting"}`

func TestClassifier_FailoverInOrder(t *testing.T) {
	exec := &fakeExecutor{
		errs: map[string]error{"a-mini": errors.New("upstream 500")},
		responses: map[string]string{
			"a-mini": "unused",
			"b-mini": validClassification,
		},
	}
	cls, err := NewClassifier(exec, []AnalyzerConfig{
		{Model: "a-mini"},
		{Model: "b-mini"},
	}, 0, 0, "/intelligent-router")
	require.NoError(t, err)

	class, used, err := cls.Classify(context.Background(), &core.ChatRequest{
		Messages: []core.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.Equal(t, "b-mini", used.Model)
	require.Equal(t, "low", class.Complexity)
	require.Equal(t, "cheap", class.SuggestedTier)
	require.InDelta(t, 0.9, class.Confidence, 1e-9)
	require.Equal(t, []string{"a-mini", "b-mini"}, exec.calls)
}

func TestClassifier_AllFailReturnsError(t *testing.T) {
	exec := &fakeExecutor{
		errs: map[string]error{
			"a-mini": errors.New("boom"),
			"b-mini": errors.New("boom"),
		},
	}
	cls, err := NewClassifier(exec, []AnalyzerConfig{{Model: "a-mini"}, {Model: "b-mini"}}, 0, 0, "")
	require.NoError(t, err)

	_, _, err = cls.Classify(context.Background(), &core.ChatRequest{
		Messages: []core.Message{{Role: "user", Content: "hi"}},
	})
	require.Error(t, err)
	require.Equal(t, []string{"a-mini", "b-mini"}, exec.calls)
}

func TestClassifier_MalformedJSONFailsOver(t *testing.T) {
	exec := &fakeExecutor{
		responses: map[string]string{
			"a-mini": "not json at all",
			"b-mini": validClassification,
		},
	}
	cls, err := NewClassifier(exec, []AnalyzerConfig{{Model: "a-mini"}, {Model: "b-mini"}}, 0, 0, "")
	require.NoError(t, err)

	class, used, err := cls.Classify(context.Background(), &core.ChatRequest{
		Messages: []core.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.Equal(t, "b-mini", used.Model)
	require.Equal(t, "chat", class.TaskType)
	// The first analyzer is called twice: initial attempt + one repair attempt,
	// then failover continues to the next analyzer.
	require.Equal(t, []string{"a-mini", "a-mini", "b-mini"}, exec.calls)
}

func TestParseClassification_ToleratesCodeFence(t *testing.T) {
	class, err := parseClassification("```json\n" + validClassification + "\n```")
	require.NoError(t, err)
	require.Equal(t, "cheap", class.SuggestedTier)
	require.InDelta(t, 0.9, class.Confidence, 1e-9)
}

func TestParseClassification_RejectsGarbage(t *testing.T) {
	_, err := parseClassification("totally not json")
	require.Error(t, err)
}

func TestNewClassifier_RequiresExecutorAndAnalyzer(t *testing.T) {
	_, err := NewClassifier(nil, []AnalyzerConfig{{Model: "a"}}, 0, 0, "")
	require.Error(t, err)
	_, err = NewClassifier(&fakeExecutor{}, nil, 0, 0, "")
	require.Error(t, err)
}

func TestAnalyzerUserPrompt_IncludesRoutingGuidanceWhenPresent(t *testing.T) {
	guide := "Use for complex reasoning and architecture"
	candidates := []Candidate{
		{
			Selector: core.ModelSelector{Provider: "anthropic", Model: "claude-opus-4-8"},
			Model: &core.Model{Metadata: &core.ModelMetadata{RoutingGuidance: guide}},
		},
		{
			Selector: core.ModelSelector{Provider: "anthropic", Model: "claude-haiku-4-5"},
			Model: &core.Model{Metadata: &core.ModelMetadata{}},
		},
	}
	prompt := analyzerUserPrompt(&core.ChatRequest{
		Messages: []core.Message{{Role: "user", Content: "help me design a system"}},
	}, candidates, nil)
	require.Contains(t, prompt, "Available models:")
	require.Contains(t, prompt, "anthropic/claude-opus-4-8")
	require.Contains(t, prompt, guide)
	require.NotContains(t, prompt, "anthropic/claude-haiku-4-5\n  routing_guidance")
}

func TestClassifier_AttemptsRepairBeforeFailover(t *testing.T) {
	exec := &fakeExecutor{
		responseSeq: map[string][]string{
			"a-mini": {"not json at all", validClassification},
		},
	}
	cls, err := NewClassifier(exec, []AnalyzerConfig{{Model: "a-mini"}, {Model: "b-mini"}}, 0, 0, "")
	require.NoError(t, err)

	class, used, err := cls.Classify(context.Background(), &core.ChatRequest{
		Messages: []core.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.Equal(t, "a-mini", used.Model)
	require.Equal(t, "chat", class.TaskType)
	// same analyzer called twice: initial + repair. No failover to b-mini.
	require.Equal(t, []string{"a-mini", "a-mini"}, exec.calls)
}

func TestClassifier_RepairFailureFallsBackToNextAnalyzer(t *testing.T) {
	exec := &fakeExecutor{
		responseSeq: map[string][]string{
			"a-mini": {"not json at all", "still not json"},
			"b-mini": {validClassification},
		},
	}
	cls, err := NewClassifier(exec, []AnalyzerConfig{{Model: "a-mini"}, {Model: "b-mini"}}, 0, 0, "")
	require.NoError(t, err)

	class, used, err := cls.Classify(context.Background(), &core.ChatRequest{
		Messages: []core.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	require.Equal(t, "b-mini", used.Model)
	require.Equal(t, "chat", class.TaskType)
	// a-mini initial + repair, then failover to b-mini.
	require.Equal(t, []string{"a-mini", "a-mini", "b-mini"}, exec.calls)
}
