package aliases

import (
	"strings"
	"time"

	"gomodel/internal/core"
)

// Alias maps a gateway-visible alias name to a concrete model selector.
type Alias struct {
	Name           string    `json:"name" bson:"name"`
	TargetModel    string    `json:"target_model" bson:"target_model"`
	TargetProvider string    `json:"target_provider,omitempty" bson:"target_provider,omitempty"`
	Description string    `json:"description,omitempty" bson:"description,omitempty"`
	Enabled        bool      `json:"enabled" bson:"enabled"`
	UserPaths      []string  `json:"user_paths,omitempty" bson:"user_paths,omitempty"`
	CreatedAt      time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" bson:"updated_at"`
}

// MatchesUserPath reports whether the given userPath is allowed to use this alias.
// An empty or single-element list containing "/" matches all user paths.
func (a Alias) MatchesUserPath(userPath string) bool {
	if len(a.UserPaths) == 0 {
		return true
	}
	for _, allowed := range a.UserPaths {
		if allowed == "/" {
			return true
		}
		if userPath == allowed {
			return true
		}
		// Check if userPath is a descendant of allowed path
		if strings.HasPrefix(userPath, allowed+"/") {
			return true
		}
	}
	return false
}

// TargetSelector returns the concrete selector this alias points to.
func (a Alias) TargetSelector() (core.ModelSelector, error) {
	return core.ParseModelSelector(a.TargetModel, a.TargetProvider)
}

// Resolution captures the requested selector and the concrete selector chosen after alias resolution.
type Resolution struct {
	Requested core.ModelSelector `json:"requested"`
	Resolved  core.ModelSelector `json:"resolved"`
	Alias     *Alias             `json:"alias,omitempty"`
}

// View is the admin-facing representation of an alias with current validity status.
type View struct {
	Alias
	ResolvedModel            string `json:"resolved_model"`
	ProviderType             string `json:"provider_type,omitempty"`
	Valid                    bool   `json:"valid"`
	HasUserPathRestriction   bool   `json:"has_user_path_restriction"`
}
