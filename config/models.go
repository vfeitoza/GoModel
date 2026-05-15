package config

import "strings"

// ModelsConfig holds global model access defaults.
type ModelsConfig struct {
	// EnabledByDefault controls whether provider models are available
	// when no persisted user-path override exists and model overrides are enabled.
	// Default: true.
	EnabledByDefault bool `yaml:"enabled_by_default" env:"MODELS_ENABLED_BY_DEFAULT"`

	// OverridesEnabled controls whether persisted model access overrides are
	// loaded, enforced, and exposed through the admin dashboard/API.
	// Default: true.
	OverridesEnabled bool `yaml:"overrides_enabled" env:"MODEL_OVERRIDES_ENABLED"`

	// KeepOnlyAliasesAtModelsEndpoint controls whether GET /v1/models hides
	// provider models and returns only alias-projected model entries.
	// Default: false.
	KeepOnlyAliasesAtModelsEndpoint bool `yaml:"keep_only_aliases_at_models_endpoint" env:"KEEP_ONLY_ALIASES_AT_MODELS_ENDPOINT"`

	// ConfiguredProviderModelsMode controls how providers.<name>.models and
	// provider *_MODELS env vars affect the provider model inventory.
	// Supported values: "fallback", "allowlist". Default: "fallback".
	ConfiguredProviderModelsMode ConfiguredProviderModelsMode `yaml:"configured_provider_models_mode" env:"CONFIGURED_PROVIDER_MODELS_MODE"`

	// ModelsEndpointIDFormat controls the model ID format returned by GET /v1/models.
	// Supported values: "qualified" (provider/model), "unqualified" (model), "both".
	// Default: "qualified".
	ModelsEndpointIDFormat ModelsEndpointIDFormat `yaml:"models_endpoint_id_format" env:"MODELS_ENDPOINT_ID_FORMAT"`
}

// ConfiguredProviderModelsMode controls how explicitly configured provider
// model lists are applied to the discovered model inventory.
type ConfiguredProviderModelsMode string

const (
	ConfiguredProviderModelsModeFallback  ConfiguredProviderModelsMode = "fallback"
	ConfiguredProviderModelsModeAllowlist ConfiguredProviderModelsMode = "allowlist"
)

// Valid reports whether mode is one of the supported configured-provider-models modes.
func (m ConfiguredProviderModelsMode) Valid() bool {
	switch NormalizeConfiguredProviderModelsMode(m) {
	case ConfiguredProviderModelsModeFallback, ConfiguredProviderModelsModeAllowlist:
		return true
	default:
		return false
	}
}

// NormalizeConfiguredProviderModelsMode canonicalizes a configured provider models mode.
func NormalizeConfiguredProviderModelsMode(mode ConfiguredProviderModelsMode) ConfiguredProviderModelsMode {
	return ConfiguredProviderModelsMode(strings.ToLower(strings.TrimSpace(string(mode))))
}

// ResolveConfiguredProviderModelsMode canonicalizes mode and applies the process default.
func ResolveConfiguredProviderModelsMode(mode ConfiguredProviderModelsMode) ConfiguredProviderModelsMode {
	mode = NormalizeConfiguredProviderModelsMode(mode)
	if mode == "" {
		return ConfiguredProviderModelsModeFallback
	}
	return mode
}

// ModelsEndpointIDFormat controls the model ID format returned by GET /v1/models.
type ModelsEndpointIDFormat string

const (
	ModelsEndpointIDFormatQualified   ModelsEndpointIDFormat = "qualified"
	ModelsEndpointIDFormatUnqualified ModelsEndpointIDFormat = "unqualified"
	ModelsEndpointIDFormatBoth        ModelsEndpointIDFormat = "both"
)

// Valid reports whether f is one of the supported models endpoint ID formats.
func (f ModelsEndpointIDFormat) Valid() bool {
	switch NormalizeModelsEndpointIDFormat(f) {
	case ModelsEndpointIDFormatQualified, ModelsEndpointIDFormatUnqualified, ModelsEndpointIDFormatBoth:
		return true
	default:
		return false
	}
}

// NormalizeModelsEndpointIDFormat canonicalizes a models endpoint ID format value.
func NormalizeModelsEndpointIDFormat(f ModelsEndpointIDFormat) ModelsEndpointIDFormat {
	return ModelsEndpointIDFormat(strings.ToLower(strings.TrimSpace(string(f))))
}

// ResolveModelsEndpointIDFormat canonicalizes f and applies the process default.
func ResolveModelsEndpointIDFormat(f ModelsEndpointIDFormat) ModelsEndpointIDFormat {
	f = NormalizeModelsEndpointIDFormat(f)
	if f == "" {
		return ModelsEndpointIDFormatQualified
	}
	return f
}
