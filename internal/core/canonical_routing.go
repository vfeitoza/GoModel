package core

// CanonicalRoutingResolution captures pool-based model routing metadata.
type CanonicalRoutingResolution struct {
	CanonicalModel       string
	Primary              ModelSelector
	Fallbacks            []ModelSelector
	Strategy             string
	ConfigPrimary        ModelSelector
	EffectiveCandidate   ModelSelector
	BlockedCandidates    []BlockedCandidate
	SelectedProviderName string
	SelectedExactModel   string
	FailoverUsed         bool
	FallbackTarget       string
}

type BlockedCandidate struct {
	Selector ModelSelector
	Reason   string
	Status   string
}

// CanonicalRoutingResolver optionally exposes canonical pool resolution metadata.
type CanonicalRoutingResolver interface {
	Resolve(requested RequestedModelSelector) (*CanonicalRoutingResolution, bool, error)
}
