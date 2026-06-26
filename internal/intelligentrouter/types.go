// Package intelligentrouter classifies an incoming request with a cheap
// analyzer model and selects the best catalog model for execution. It is
// transport-free: it does not know about Echo, HTTP, storage, or specific
// providers. The analyzer call is made through a ChatCompletionExecutor that
// reuses the gateway's own routing, authorization, fallback, and usage logic.
//
// See docs/dev/intelligent-model.md for the full design and rollout phases.
package intelligentrouter

import (
	"context"
	"time"

	"gomodel/internal/core"
)

// Mode controls how a routing decision is applied.
const (
	ModeOff     = "off"
	ModeObserve = "observe"
	ModeEnforce = "enforce"
)

// Selection strategy values mirror config.IntelligentStrategy*.
const (
	StrategyCost     = "cost"
	StrategyBalanced = "balanced"
	StrategyQuality  = "quality"
	StrategyLatency  = "latency"
)

// Classification is the analyzer's structured read of the request.
type Classification struct {
	Complexity          string // low | medium | high
	TaskType            string // chat | summary | coding | reasoning | extraction | translation | creative | vision | audio | tool_use | other
	RequiresReasoning   bool
	RequiresCode        bool
	RequiresLongContext bool
	RequiresVision      bool
	RequiresTools       bool
	QualitySensitivity  string // low | medium | high
	SuggestedTier       string // cheap | standard | premium
	Confidence          float64
	Reason              string
}

// SelectionMeta carries transport-derived context into the selector.
type SelectionMeta struct {
	// Strategy overrides the configured default for this request.
	Strategy string
	// Mode is the resolved routing mode (off/observe/enforce).
	Mode string
	// UserPath is the effective request user_path, used for candidate filtering.
	UserPath string
	// Endpoint is the request endpoint operation (e.g. openai.chat_completions).
	Endpoint string
	// CandidateAllow overrides the configured allow list (used by intelligent
	// virtual models to restrict selection to their targets).
	CandidateAllow []string
}

// Decision records one intelligent routing decision.
type Decision struct {
	Requested      core.RequestedModelSelector
	Analyzers      []core.ModelSelector // pool tried, in order
	AnalyzerUsed   core.ModelSelector   // zero when analysis failed
	SelectedModel  core.ModelSelector   // the recommendation (or fallback) the router produced
	AppliedModel   core.ModelSelector   // the model the gateway should actually execute
	Applied        bool                 // true when AppliedModel should replace the requested selector (enforce)
	Strategy       string
	Reason         string
	Confidence     float64
	Mode           string // off | observe | enforce
	AnalysisFailed bool
	Duration       time.Duration
	Classification *Classification // nil when analysis failed or was skipped
}

// ChatCompletionExecutor executes a single internal chat completion. It mirrors
// guardrails.ChatCompletionExecutor to avoid an import cycle with that package.
type ChatCompletionExecutor interface {
	ChatCompletion(ctx context.Context, req *core.ChatRequest) (*core.ChatResponse, error)
}

// Catalog lists eligible models with their metadata for ranking. It is a subset
// of core.ModelLookup focused on the fields the scorer needs.
type Catalog interface {
	ListModels() []core.Model
	Supports(model string) bool
}

// PricingResolver returns effective pricing for a model, or nil when unknown.
// It mirrors usage.PricingResolver without the import.
type PricingResolver interface {
	ResolvePricing(model, providerType string) *core.ModelPricing
}

// VirtualTargetResolver exposes the candidate targets of an intelligent virtual
// model. It is implemented by an adapter over the virtualmodels service so this
// package does not import virtualmodels directly.
type VirtualTargetResolver interface {
	// IntelligentTargets returns the candidate targets and resolved strategy for
	// an intelligent virtual model named source, scoped to the request user path.
	// ok is false when source is not an intelligent virtual model.
	IntelligentTargets(source, userPath string) (targets []core.ModelSelector, strategy string, ok bool)
}
