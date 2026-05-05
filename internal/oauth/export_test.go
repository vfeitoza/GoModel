package oauth

// Test helpers — exported only for use in _test packages.
// These allow injecting custom URLs in unit tests without exposing them
// in the production API.

// NewAnthropicProviderWithTestTokenURL creates an AnthropicProvider that
// sends token requests to the given URL instead of the real Anthropic endpoint.
func NewAnthropicProviderWithTestTokenURL(tokenURL string) *AnthropicProvider {
	p := NewAnthropicProvider()
	p.tokenURL = tokenURL
	return p
}

// NewAnthropicProviderWithTestProfileURL creates an AnthropicProvider that
// sends profile requests to the given URL instead of the real Anthropic endpoint.
func NewAnthropicProviderWithTestProfileURL(profileURL string) *AnthropicProvider {
	p := NewAnthropicProvider()
	p.profileURL = profileURL
	return p
}
