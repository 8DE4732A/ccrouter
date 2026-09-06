package db

import (
	"database/sql"
	"time"
)

// McpUserToken stores an OAuth access and refresh token for a specific user and MCP provider.
type McpUserToken struct {
	ID           int64  `json:"id"`
	Provider     string `json:"provider"`
	UserID       string `json:"user_id"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	Scopes       string `json:"scopes,omitempty"`
	ExpiresAt    *int64 `json:"expires_at,omitempty"`
	UpdatedAt    int64  `json:"updated_at"`
}

// SaveMcpToken inserts or updates an MCP user token.
func (r *Recorder) SaveMcpToken(tok *McpUserToken) error {
	now := time.Now().Unix()
	tok.UpdatedAt = now
	if tok.TokenType == "" {
		tok.TokenType = "Bearer"
	}

	query := `
INSERT INTO mcp_user_tokens (provider, user_id, access_token, refresh_token, token_type, scopes, expires_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, user_id) DO UPDATE SET
  access_token = excluded.access_token,
  refresh_token = excluded.refresh_token,
  token_type = excluded.token_type,
  scopes = excluded.scopes,
  expires_at = excluded.expires_at,
  updated_at = excluded.updated_at
`
	res, err := r.writeConn.Exec(query, tok.Provider, tok.UserID, tok.AccessToken, tok.RefreshToken, tok.TokenType, tok.Scopes, tok.ExpiresAt, tok.UpdatedAt)
	if err != nil {
		return err
	}
	if tok.ID == 0 {
		id, err := res.LastInsertId()
		if err == nil {
			tok.ID = id
		}
	}
	return nil
}

// GetMcpToken retrieves the token for a specific provider and user ID.
// If not found, returns (nil, nil).
func (r *Recorder) GetMcpToken(provider, userID string) (*McpUserToken, error) {
	query := `
SELECT id, provider, user_id, access_token, refresh_token, token_type, scopes, expires_at, updated_at
FROM mcp_user_tokens
WHERE provider = ? AND user_id = ?
LIMIT 1
`
	row := r.writeConn.QueryRow(query, provider, userID)
	var tok McpUserToken
	var refreshToken, scopes sql.NullString
	var expiresAt sql.NullInt64

	err := row.Scan(&tok.ID, &tok.Provider, &tok.UserID, &tok.AccessToken, &refreshToken, &tok.TokenType, &scopes, &expiresAt, &tok.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if refreshToken.Valid {
		tok.RefreshToken = refreshToken.String
	}
	if scopes.Valid {
		tok.Scopes = scopes.String
	}
	if expiresAt.Valid {
		tok.ExpiresAt = &expiresAt.Int64
	}
	return &tok, nil
}

// DeleteMcpToken removes the token for a specific provider and user ID.
func (r *Recorder) DeleteMcpToken(provider, userID string) error {
	_, err := r.writeConn.Exec("DELETE FROM mcp_user_tokens WHERE provider = ? AND user_id = ?", provider, userID)
	return err
}

// DeleteMcpTokenByID removes a token by its primary key ID.
func (r *Recorder) DeleteMcpTokenByID(id int64) error {
	_, err := r.writeConn.Exec("DELETE FROM mcp_user_tokens WHERE id = ?", id)
	return err
}

// ListMcpTokens returns all stored user tokens ordered by updated_at DESC.
func (r *Recorder) ListMcpTokens() ([]*McpUserToken, error) {
	query := `
SELECT id, provider, user_id, access_token, refresh_token, token_type, scopes, expires_at, updated_at
FROM mcp_user_tokens
ORDER BY updated_at DESC
`
	rows, err := r.writeConn.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []*McpUserToken
	for rows.Next() {
		var tok McpUserToken
		var refreshToken, scopes sql.NullString
		var expiresAt sql.NullInt64

		if err := rows.Scan(&tok.ID, &tok.Provider, &tok.UserID, &tok.AccessToken, &refreshToken, &tok.TokenType, &scopes, &expiresAt, &tok.UpdatedAt); err != nil {
			return nil, err
		}
		if refreshToken.Valid {
			tok.RefreshToken = refreshToken.String
		}
		if scopes.Valid {
			tok.Scopes = scopes.String
		}
		if expiresAt.Valid {
			tok.ExpiresAt = &expiresAt.Int64
		}
		tokens = append(tokens, &tok)
	}
	return tokens, rows.Err()
}
