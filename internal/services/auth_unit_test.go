// Package services auth domain unit tests: service logic against mocked
// repositories and a fake transaction runner. No database required.
package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"fmis-api/internal/middleware"
	"fmis-api/internal/models"
	"fmis-api/internal/repositories"
	"fmis-api/internal/schemas"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newMockAuthService builds an AuthService with mocked stores.
func newMockAuthService(users UserStore, refresh RefreshTokenStore) *AuthService {
	return NewAuthServiceWithDeps(stubQuerier{}, fakeTx, users, refresh, "test-secret", 15*time.Minute, 7*24*time.Hour)
}

// testUser returns a minimal active user row.
func testUser() models.User {
	return models.User{
		ID:       uuid.MustParse("f47ac10b-58cc-4372-a567-0e02b2c3d479"),
		Username: "tester",
		Email:    "tester@example.com",
		Role:     models.UserRoleTypeViewer,
		IsActive: true,
	}
}

func TestRegisterUnitHappyPath(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	user := testUser()
	users.On("CreateUser", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			p := args.Get(2).(repositories.CreateUserParams)
			assert.Equal(t, "tester", p.Username)
			assert.Equal(t, "tester@example.com", p.Email)
			assert.True(t, strings.HasPrefix(p.PasswordHash, "$2"), "password must be stored as bcrypt")
			assert.NotEqual(t, "correct-horse-battery", p.PasswordHash)
			assert.Equal(t, models.UserRoleTypeViewer, p.Role, "self-registration always creates a VIEWER")
		}).
		Return(user, nil).Once()
	refresh.On("CreateRefreshToken", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			p := args.Get(2).(repositories.CreateRefreshTokenParams)
			assert.Equal(t, user.ID, p.UserID)
			assert.Len(t, p.TokenHash, 64, "refresh tokens must be stored as a SHA-256 hash")
		}).
		Return(models.RefreshToken{}, nil).Once()

	resp, err := svc.Register(context.Background(), schemas.RegisterRequest{
		Username: "tester",
		Email:    "tester@example.com",
		Password: "correct-horse-battery",
	})
	require.NoError(t, err)

	assert.NotEmpty(t, resp.AccessToken)
	assert.Len(t, resp.RefreshToken, 64)
	assert.Equal(t, "Bearer", resp.TokenType)
	assert.Equal(t, int64(900), resp.ExpiresIn)
	users.AssertExpectations(t)
	refresh.AssertExpectations(t)
}

func TestRegisterUnitDuplicate(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	users.On("CreateUser", mock.Anything, mock.Anything, mock.Anything).
		Return(models.User{}, &pgconn.PgError{Code: "23505"}).Once()

	_, err := svc.Register(context.Background(), schemas.RegisterRequest{
		Username: "taken",
		Email:    "taken@example.com",
		Password: "correct-horse-battery",
	})
	assert.ErrorIs(t, err, ErrDuplicate)
	refresh.AssertNotCalled(t, "CreateRefreshToken", mock.Anything, mock.Anything, mock.Anything)
}

func TestLoginUnitHappyPath(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	hash, err := hashPassword("correct-horse-battery")
	require.NoError(t, err)
	user := testUser()
	user.PasswordHash = hash

	users.On("GetUserByIdentifier", mock.Anything, mock.Anything, "tester").Return(user, nil).Once()
	refresh.On("CreateRefreshToken", mock.Anything, mock.Anything, mock.Anything).
		Return(models.RefreshToken{}, nil).Once()

	resp, err := svc.Login(context.Background(), schemas.LoginRequest{
		Identifier: "tester",
		Password:   "correct-horse-battery",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)
	assert.Len(t, resp.RefreshToken, 64)
}

func TestLoginUnitRejectsUnknownUser(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	users.On("GetUserByIdentifier", mock.Anything, mock.Anything, "ghost").
		Return(models.User{}, pgx.ErrNoRows).Once()

	_, err := svc.Login(context.Background(), schemas.LoginRequest{Identifier: "ghost", Password: "whatever"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)
	refresh.AssertNotCalled(t, "CreateRefreshToken", mock.Anything, mock.Anything, mock.Anything)
}

func TestLoginUnitRejectsWrongPassword(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	hash, err := hashPassword("correct-horse-battery")
	require.NoError(t, err)
	user := testUser()
	user.PasswordHash = hash

	users.On("GetUserByIdentifier", mock.Anything, mock.Anything, "tester").Return(user, nil).Once()

	_, err = svc.Login(context.Background(), schemas.LoginRequest{Identifier: "tester", Password: "wrong-password"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)
	refresh.AssertNotCalled(t, "CreateRefreshToken", mock.Anything, mock.Anything, mock.Anything)
}

func TestLoginUnitRejectsInactiveUser(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	hash, err := hashPassword("correct-horse-battery")
	require.NoError(t, err)
	user := testUser()
	user.PasswordHash = hash
	user.IsActive = false

	users.On("GetUserByIdentifier", mock.Anything, mock.Anything, "tester").Return(user, nil).Once()

	_, err = svc.Login(context.Background(), schemas.LoginRequest{Identifier: "tester", Password: "correct-horse-battery"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestRefreshUnitRotatesToken(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	oldRaw := "aa" + strings.Repeat("0", 62)
	oldHash := hashToken(oldRaw)
	user := testUser()
	valid := models.RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: oldHash,
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	refresh.On("FindByHash", mock.Anything, mock.Anything, oldHash).Return(valid, nil).Once()
	users.On("GetUserByID", mock.Anything, mock.Anything, user.ID).Return(user, nil).Once()
	refresh.On("Revoke", mock.Anything, mock.Anything, oldHash).Return(nil).Once()

	var rotatedHash string
	refresh.On("CreateRefreshToken", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			p := args.Get(2).(repositories.CreateRefreshTokenParams)
			rotatedHash = p.TokenHash
			assert.Equal(t, user.ID, p.UserID)
			assert.NotEqual(t, oldHash, p.TokenHash, "rotation must issue a fresh hash")
		}).
		Return(models.RefreshToken{}, nil).Once()

	resp, err := svc.Refresh(context.Background(), oldRaw)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.AccessToken)
	assert.NotEqual(t, oldRaw, resp.RefreshToken, "the raw token must rotate")
	assert.Equal(t, hashToken(resp.RefreshToken), rotatedHash, "stored hash must match the returned token")

	claims := &middleware.Claims{}
	parsed, err := jwt.ParseWithClaims(resp.AccessToken, claims,
		func(_ *jwt.Token) (any, error) { return []byte("test-secret"), nil },
		jwt.WithValidMethods([]string{"HS256"}),
	)
	require.NoError(t, err)
	assert.True(t, parsed.Valid)
	assert.Equal(t, user.ID.String(), claims.UserID)
	users.AssertExpectations(t)
	refresh.AssertExpectations(t)
}

func TestRefreshUnitRejectsUnknownToken(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	refresh.On("FindByHash", mock.Anything, mock.Anything, mock.Anything).
		Return(models.RefreshToken{}, pgx.ErrNoRows).Once()

	_, err := svc.Refresh(context.Background(), "deadbeef")
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	users.AssertNotCalled(t, "GetUserByID", mock.Anything, mock.Anything, mock.Anything)
	refresh.AssertNotCalled(t, "Revoke", mock.Anything, mock.Anything, mock.Anything)
}

func TestRefreshUnitRejectsRevokedToken(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	raw := "raw-token"
	revoked := models.RefreshToken{
		ID:        uuid.New(),
		UserID:    testUser().ID,
		TokenHash: hashToken(raw),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Revoked:   true,
	}
	refresh.On("FindByHash", mock.Anything, mock.Anything, hashToken(raw)).Return(revoked, nil).Once()

	_, err := svc.Refresh(context.Background(), raw)
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	refresh.AssertNotCalled(t, "Revoke", mock.Anything, mock.Anything, mock.Anything)
	refresh.AssertNotCalled(t, "CreateRefreshToken", mock.Anything, mock.Anything, mock.Anything)
}

func TestRefreshUnitRejectsExpiredToken(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	raw := "raw-token"
	expired := models.RefreshToken{
		ID:        uuid.New(),
		UserID:    testUser().ID,
		TokenHash: hashToken(raw),
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	refresh.On("FindByHash", mock.Anything, mock.Anything, hashToken(raw)).Return(expired, nil).Once()

	_, err := svc.Refresh(context.Background(), raw)
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	refresh.AssertNotCalled(t, "Revoke", mock.Anything, mock.Anything, mock.Anything)
}

func TestRefreshUnitRejectsInactiveUser(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	raw := "raw-token"
	user := testUser()
	user.IsActive = false
	valid := models.RefreshToken{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: hashToken(raw),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	refresh.On("FindByHash", mock.Anything, mock.Anything, hashToken(raw)).Return(valid, nil).Once()
	users.On("GetUserByID", mock.Anything, mock.Anything, user.ID).Return(user, nil).Once()

	_, err := svc.Refresh(context.Background(), raw)
	assert.ErrorIs(t, err, ErrInvalidRefreshToken)
	refresh.AssertNotCalled(t, "Revoke", mock.Anything, mock.Anything, mock.Anything)
}

func TestLogoutUnitRevokesToken(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	raw := "bb" + strings.Repeat("0", 62)
	refresh.On("Revoke", mock.Anything, mock.Anything, hashToken(raw)).Return(nil).Once()

	err := svc.Logout(context.Background(), raw)
	assert.NoError(t, err)
	refresh.AssertExpectations(t)
}

func TestLogoutUnitIgnoresUnknownToken(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	refresh.On("Revoke", mock.Anything, mock.Anything, mock.Anything).Return(pgx.ErrNoRows).Once()

	err := svc.Logout(context.Background(), "unknown")
	assert.NoError(t, err, "logout stays idempotent for unknown tokens")
}

func TestMeUnit(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	user := testUser()
	users.On("GetUserByID", mock.Anything, mock.Anything, user.ID).Return(user, nil).Once()

	me, err := svc.Me(context.Background(), user.ID.String())
	require.NoError(t, err)
	assert.Equal(t, user.ID.String(), me.ID)
	assert.Equal(t, "tester", me.Username)
	assert.Equal(t, "tester@example.com", me.Email)
	assert.Equal(t, string(models.UserRoleTypeViewer), me.Role)
}

func TestMeUnitRejectsUnknownUser(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	users.On("GetUserByID", mock.Anything, mock.Anything, mock.Anything).
		Return(models.User{}, pgx.ErrNoRows).Once()

	_, err := svc.Me(context.Background(), uuid.New().String())
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestMeUnitRejectsMalformedID(t *testing.T) {
	users := new(MockUserStore)
	refresh := new(MockRefreshTokenStore)
	svc := newMockAuthService(users, refresh)

	_, err := svc.Me(context.Background(), "not-a-uuid")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
	users.AssertNotCalled(t, "GetUserByID", mock.Anything, mock.Anything, mock.Anything)
}
