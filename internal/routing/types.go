package routing

import (
	"strings"

	"gomodel/internal/core"
	"gomodel/internal/providers"
)

type Registry interface {
	GetModel(model string) any
	GetProviderTypeForName(providerName string) string
}

type RuntimeSnapshotProvider interface {
	ProviderRuntimeSnapshots() []providers.ProviderRuntimeSnapshot
}

type Candidate struct {
	Provider string
	Model    string
	Priority int
	Weight   int
}

func (c Candidate) Selector() core.ModelSelector {
	return core.ModelSelector{Provider: c.Provider, Model: c.Model}
}

func (c Candidate) QualifiedModel() string {
	return c.Selector().QualifiedModel()
}

type Pool struct {
	CanonicalModel string
	Candidates     []Candidate
}

func normalizePoolKey(model string) string {
	return strings.TrimSpace(model)
}
