// Package repositories executes parameterized SQL queries against PostgreSQL.
package repositories

import (
	"context"
	"fmis-api/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateUserParams carries the values needed to insert a new user row.
type CreateUserParams struct {
	Username     string
	Email        string
	PasswordHash string
	Role         models.UserRoleType
}

// UserRepository reads and writes the users table.
type UserRepository struct{}

// CreateUser inserts a user and returns the full stored row.
func (r *UserRepository) CreateUser(ctx context.Context, q Querier, p CreateUserParams) (models.User, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, role)
		VALUES ($1, $2, $3, $4)
		RETURNING id, username, email, password_hash, role, is_active, created_at, updated_at`,
		p.Username, p.Email, p.PasswordHash, p.Role)
	return scanUser(row)
}

// GetUserByIdentifier fetches a user by username OR email (login path).
func (r *UserRepository) GetUserByIdentifier(ctx context.Context, q Querier, identifier string) (models.User, error) {
	row := q.QueryRow(ctx, `
		SELECT id, username, email, password_hash, role, is_active, created_at, updated_at
		FROM users
		WHERE username = $1 OR email = $1`,
		identifier)
	return scanUser(row)
}

// GetUserByID fetches a user by ID.
func (r *UserRepository) GetUserByID(ctx context.Context, q Querier, id uuid.UUID) (models.User, error) {
	row := q.QueryRow(ctx, `
		SELECT username, email, password_hash, role, is_active, created_at, updated_at
		FROM users
		WHERE id = $1`,
		id)
	return scanUser(row)
}

// scanUser maps one users row into a models.User.
func scanUser(row pgx.Row) (models.User, error) {
	var u models.User
	err := row.Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash,
		&u.Role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt,
	)
	return u, err
}
