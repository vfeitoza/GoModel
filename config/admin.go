package config

// AdminConfig holds configuration for the admin API and dashboard UI.
type AdminConfig struct {
	// EndpointsEnabled controls whether the admin REST API is active
	// Default: true
	EndpointsEnabled bool `yaml:"endpoints_enabled" env:"ADMIN_ENDPOINTS_ENABLED"`

	// UIEnabled controls whether the admin dashboard UI is active
	// Requires EndpointsEnabled — if endpoints are disabled and UI is enabled,
	// a warning is logged and UI is forced to false.
	// Default: true
	UIEnabled bool `yaml:"ui_enabled" env:"ADMIN_UI_ENABLED"`
}
