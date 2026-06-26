package gateway

import (
	"context"
	"log/slog"
	"reflect"
	"strings"

	"gomodel/internal/core"
	"gomodel/internal/intelligentrouter"
)

// evaluateIntelligentRouter asks the intelligent router (when configured) to
// evaluate the request and, when it is applied (enforce mode), rewrites the
// selector pointers to the chosen model before normal resolution runs. The
// rewritten selector still passes through authorization and provider routing.
//
// Returns the effective selector the orchestrator should resolve next. When the
// feature is disabled, the request is not an intelligent selector, or the
// router is in observe mode, the requested selector is returned unchanged.
func (o *InferenceOrchestrator) evaluateIntelligentRouter(
	ctx context.Context,
	req any,
	requested core.RequestedModelSelector,
) core.RequestedModelSelector {
	if isNilIntelligentRouter(o.intelligentRouter) {
		return requested
	}
	chatReq, ok := req.(*core.ChatRequest)
	if !ok {
		// Intelligent routing currently classifies chat requests only; other
		// request types fall through to normal resolution.
		return requested
	}
	// Only invoke the analyzer for intelligent selectors/virtual models.
	meta := intelligentrouter.SelectionMeta{
		UserPath: core.UserPathFromContext(ctx),
	}
	strategy, ok := o.intelligentRouter.ShouldEvaluate(requested, meta)
	if !ok {
		return requested
	}
	meta.Strategy = strategy
	decision := o.intelligentRouter.Evaluate(ctx, chatReq, requested, meta)
	if decision == nil || !decision.Applied {
		return requested
	}
	applied := decision.AppliedModel
	if applied.Model == "" {
		return requested
	}
	slog.Info("intelligent routing applied",
		"from", requested.RequestedQualifiedModel(),
		"to", applied.QualifiedModel(),
		"analysis_failed", decision.AnalysisFailed,
		"reason", decision.Reason,
	)
	// Preserve an explicit provider hint only when the router selected one.
	hint := strings.TrimSpace(applied.Provider)
	return core.NewRequestedModelSelector(applied.Model, hint)
}

// isNilIntelligentRouter reports whether the router is absent. It handles the
// typed-nil case: an interface assigned a concrete (*Selector)(nil) is not == nil,
// so reflecting on the underlying value prevents a panic if a typed nil ever
// reaches the orchestrator. The construction site in the app package already
// keeps the field at a true nil interface, but this guards against future
// regressions.
func isNilIntelligentRouter(r IntelligentRouter) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	return v.Kind() == reflect.Ptr && v.IsNil()
}
