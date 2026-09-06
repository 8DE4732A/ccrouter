package auth

// Credential represents an authorization header and token to inject into an upstream MCP request.
type Credential struct {
	HeaderName  string
	HeaderValue string
	Token       string
	UserID      string
}
