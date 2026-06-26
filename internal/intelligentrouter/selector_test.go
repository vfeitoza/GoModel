package intelligentrouter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"gomodel/internal/core"
)

func newTestSelector(t *testing.T, mode string, exec ChatCompletionExecutor) *Selector {
	t.Helper()
	cls, err := NewClassifier(exec, []AnalyzerConfig{{Model: "a-mini"}}, 0, 0, "")
	require.NoError(t, err)
	s := NewSelector(Config{
		Classifier: cls,
		Catalog:    catalog(),
		Mode:       mode,
	})
	require.NotNil(t, s)
	return s
}

func TestNewSelector_OffModeReturnsNil(t *testing.T) {
	cls, _ := NewClassifier(&fakeExecutor{responses: map[string]string{"a-mini": validClassification}}, []AnalyzerConfig{{Model: "a-mini"}}, 0, 0, "")
	require.Nil(t, NewSelector(Config{Classifier: cls, Catalog: catalog(), Mode: ModeOff}))
}

func TestSelector_ObserveKeepsRequestedModel(t *testing.T) {
	exec := &fakeExecutor{responses: map[string]string{"a-mini": validClassification}}
	s := newTestSelector(t, ModeObserve, exec)

	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: "hi"}}}
	requested := core.NewRequestedModelSelector("auto", "")
	d := s.Evaluate(context.Background(), req, requested, SelectionMeta{Mode: ModeObserve})

	require.Equal(t, ModeObserve, d.Mode)
	require.False(t, d.Applied) // observe never replaces the requested model
	require.Equal(t, "auto", d.AppliedModel.Model)
	require.Equal(t, "mini", d.SelectedModel.Model) // recommendation is still recorded
	require.NotNil(t, d.Classification)
	require.False(t, d.AnalysisFailed)
	require.Equal(t, "a-mini", d.AnalyzerUsed.Model)
}

func TestSelector_EnforceSelectsCheapForSimple(t *testing.T) {
	exec := &fakeExecutor{responses: map[string]string{"a-mini": validClassification}}
	s := newTestSelector(t, ModeEnforce, exec)

	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: "hi"}}}
	requested := core.NewRequestedModelSelector("auto", "")
	d := s.Evaluate(context.Background(), req, requested, SelectionMeta{Mode: ModeEnforce})

	require.False(t, d.AnalysisFailed)
	require.True(t, d.Applied)
	require.Equal(t, "mini", d.AppliedModel.Model) // balanced picks cheap for low complexity
}

func TestSelector_EnforceSelectsPremiumForComplex(t *testing.T) {
	complex := `{"complexity":"high","task_type":"reasoning","requires_reasoning":true,"requires_code":false,"requires_long_context":false,"requires_vision":false,"requires_tools":false,"quality_sensitivity":"high","suggested_tier":"premium","confidence":0.95,"reason":"hard reasoning"}`
	exec := &fakeExecutor{responses: map[string]string{"a-mini": complex}}
	s := newTestSelector(t, ModeEnforce, exec)

	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: "prove the theorem"}}}
	requested := core.NewRequestedModelSelector("auto", "")
	d := s.Evaluate(context.Background(), req, requested, SelectionMeta{Mode: ModeEnforce})

	require.Equal(t, "frontier", d.AppliedModel.Model)
}

func TestSelector_AnalysisFailureUsesFallback(t *testing.T) {
	exec := &fakeExecutor{errs: map[string]error{"a-mini": errors.New("down")}}
	cls, err := NewClassifier(exec, []AnalyzerConfig{{Model: "a-mini"}}, 0, 0, "")
	require.NoError(t, err)
	s := NewSelector(Config{
		Classifier:    cls,
		Catalog:       catalog(),
		Mode:          ModeEnforce,
		FallbackModel: "pro",
	})
	require.NotNil(t, s)

	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: "hi"}}}
	requested := core.NewRequestedModelSelector("auto", "")
	d := s.Evaluate(context.Background(), req, requested, SelectionMeta{Mode: ModeEnforce})

	require.True(t, d.AnalysisFailed)
	require.True(t, d.Applied)
	require.Equal(t, "pro", d.AppliedModel.Model)
	require.Nil(t, d.Classification)
}

func TestSelector_LowConfidencePrefersStrongerModel(t *testing.T) {
	// Low confidence with a balanced strategy should not pick the cheapest.
	lowConf := `{"complexity":"medium","task_type":"chat","requires_reasoning":false,"requires_code":false,"requires_long_context":false,"requires_vision":false,"requires_tools":false,"quality_sensitivity":"medium","suggested_tier":"standard","confidence":0.3,"reason":"unsure"}`
	exec := &fakeExecutor{responses: map[string]string{"a-mini": lowConf}}
	s := newTestSelector(t, ModeEnforce, exec)

	req := &core.ChatRequest{Messages: []core.Message{{Role: "user", Content: "hmm"}}}
	requested := core.NewRequestedModelSelector("auto", "")
	d := s.Evaluate(context.Background(), req, requested, SelectionMeta{Mode: ModeEnforce})

	require.False(t, d.AnalysisFailed)
	require.NotEqual(t, "mini", d.AppliedModel.Model) // low confidence avoids the cheapest
}
