// Package repositories for refresh tokens.
package repositories

import (
	"context"
	"fmis-api/internal/models"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateRefreshTokenParams carries the values for one refresh token row.
type CreateRefreshTokenParams struct {
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
}

// RefreshTokenRepository reads and writes the refresh_tokens table.
type RefreshTokenRepository struct{}

// CreateRefreshToken stores a hashed refresh token.
func (r *RefreshTokenRepository) CreateRefreshToken(ctx context.Context, q Querier, p CreateRefreshTokenParams) (models.RefreshToken, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3)
		RETURNING id, user_id, token_hash, expires_at, revoked, created_at`,
		p.UserID, p.TokenHash, p.ExpiresAt)
	return scanRefreshToken(row)
}

// FindByHash finds a refresh token by its hash.
func (r *RefreshTokenRepository) FindByHash(ctx context.Context, q Querier, tokenHash string) (models.RefreshToken, error) {
	row := q.QueryRow(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked, created_at
		FROM refresh_tokens
		WHERE token_hash = $1`,
		tokenHash)
	return scanRefreshToken(row)
}

// Revoke revokes a refresh token by its hash.
func (r *RefreshTokenRepository) Revoke(ctx context.Context, q Querier, tokenHash string) error {
	tag, err := q.Exec(ctx, `
		UPDATE refresh_tokens
		SET revoked = true
		WHERE token_hash = $1`,
		tokenHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// scanRefreshToken maps one refresh_tokens row into a models.RefreshToken.
func scanRefreshToken(row pgx.Row) (models.RefreshToken, error) {
	var t models.RefreshToken
	err := row.Scan(
		&t.ID, &t.UserID, &t.TokenHash,
		&t.ExpiresAt, &t.Revoked, &t.CreatedAt,
	)
	return t, err
}
