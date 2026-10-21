package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/happyro/happyro-api/internal/identity"
	"strings"
	"unicode/utf8"
)

var ErrCredentials = errors.New("用户名或密码错误。")
var ErrDisabled = errors.New("账号已停用。")

type RateLimited struct{ RetryAfter int }

func (e *RateLimited) Error() string { return "登录尝试次数过多，请稍后再试。" }

type AdminRepository interface {
	ByUsername(context.Context, string) (identity.User, error)
	ByID(context.Context, int64) (identity.User, error)
	Attempt(context.Context, string) (int, error)
	LoginSucceeded(context.Context, identity.User, identity.Client, string) error
	Audit(context.Context, string, *identity.User, string, identity.Client) error
}
type AdminService struct {
	repo      AdminRepository
	passwords identity.PasswordHasher
	dummyHash string
}

func NewAdminService(repo AdminRepository, passwords identity.PasswordHasher) (*AdminService, error) {
	dummy, err := passwords.Hash("invalid-user-timing-placeholder")
	if err != nil {
		return nil, err
	}
	return &AdminService{repo: repo, passwords: passwords, dummyHash: dummy}, nil
}

func (s *AdminService) Login(ctx context.Context, username, password string, client identity.Client) (identity.User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if username == "" || len(username) > 64 || password == "" || len(password) > 255 || !utf8.ValidString(password) {
		return identity.User{}, ErrCredentials
	}
	sum := sha256.Sum256([]byte(username + "\x00" + client.IP))
	key := hex.EncodeToString(sum[:])
	retry, err := s.repo.Attempt(ctx, key)
	if err != nil {
		return identity.User{}, err
	}
	if retry > 0 {
		if err := s.repo.Audit(ctx, "auth.login_blocked", nil, username, client); err != nil {
			return identity.User{}, err
		}
		return identity.User{}, &RateLimited{retry}
	}
	user, err := s.repo.ByUsername(ctx, username)
	if err != nil && !errors.Is(err, identity.ErrNotFound) {
		return identity.User{}, err
	}
	hash := user.PasswordHash
	if errors.Is(err, identity.ErrNotFound) {
		hash = s.dummyHash
	}
	valid := s.passwords.Verify(password, hash)
	if !valid || !user.Active {
		if err := s.repo.Audit(ctx, "auth.login_failed", nil, username, client); err != nil {
			return identity.User{}, err
		}
		return identity.User{}, ErrCredentials
	}
	if err := s.repo.LoginSucceeded(ctx, user, client, key); err != nil {
		return identity.User{}, err
	}
	return user, nil
}

func (s *AdminService) Current(ctx context.Context, id int64) (identity.User, error) {
	user, err := s.repo.ByID(ctx, id)
	if err != nil {
		return identity.User{}, err
	}
	if !user.Active {
		return identity.User{}, ErrDisabled
	}
	return user, nil
}

func (s *AdminService) Logout(ctx context.Context, user identity.User, client identity.Client) error {
	return s.repo.Audit(ctx, "auth.logout", &user, user.Username, client)
}
