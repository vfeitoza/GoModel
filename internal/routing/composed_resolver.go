package routing

import (
	"context"

	"gomodel/internal/core"
)

type AliasResolver interface {
	ResolveModel(requested core.RequestedModelSelector) (core.ModelSelector, bool, error)
}

type ComposedResolver struct {
	aliasResolver AliasResolver
	poolResolver  *Resolver
}

func NewComposedResolver(aliasResolver AliasResolver, poolResolver *Resolver) *ComposedResolver {
	if aliasResolver == nil && poolResolver == nil {
		return nil
	}
	return &ComposedResolver{aliasResolver: aliasResolver, poolResolver: poolResolver}
}

func (r *ComposedResolver) ResolveModel(requested core.RequestedModelSelector) (core.ModelSelector, bool, error) {
	return r.ResolveModelWithContext(context.Background(), requested)
}

func (r *ComposedResolver) ResolveModelWithContext(ctx context.Context, requested core.RequestedModelSelector) (core.ModelSelector, bool, error) {
	requested = core.NewRequestedModelSelector(requested.Model, requested.ProviderHint)

	aliasApplied := false
	if r != nil && r.aliasResolver != nil {
		selector, changed, err := r.aliasResolver.ResolveModel(requested)
		if err != nil {
			return core.ModelSelector{}, false, err
		}
		if selector != (core.ModelSelector{}) {
			requested = core.NewRequestedModelSelector(selector.QualifiedModel(), "")
			aliasApplied = changed
		}
	}

	if r != nil && r.poolResolver != nil {
		resolution, matched, err := r.poolResolver.ResolveWithContext(ctx, requested)
		if err != nil {
			return core.ModelSelector{}, false, err
		}
		if matched && resolution != nil {
			return resolution.Primary, true, nil
		}
	}

	if r != nil && r.aliasResolver != nil {
		selector, err := requested.Normalize()
		return selector, aliasApplied, err
	}
	selector, err := requested.Normalize()
	return selector, false, err
}

func (r *ComposedResolver) Resolve(requested core.RequestedModelSelector) (*core.CanonicalRoutingResolution, bool, error) {
	return r.ResolveWithContext(context.Background(), requested)
}

func (r *ComposedResolver) ResolveWithContext(ctx context.Context, requested core.RequestedModelSelector) (*core.CanonicalRoutingResolution, bool, error) {
	requested = core.NewRequestedModelSelector(requested.Model, requested.ProviderHint)
	if r == nil || r.poolResolver == nil {
		return nil, false, nil
	}
	if r.aliasResolver != nil {
		selector, _, err := r.aliasResolver.ResolveModel(requested)
		if err != nil {
			return nil, false, err
		}
		if selector != (core.ModelSelector{}) {
			requested = core.NewRequestedModelSelector(selector.QualifiedModel(), "")
		}
	}
	return r.poolResolver.ResolveWithContext(ctx, requested)
}
