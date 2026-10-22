package identity

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

var ErrNotFound = errors.New("账号不存在。")

type User struct {
	ID           int64    `db:"id" json:"id"`
	Username     string   `db:"username" json:"username"`
	Name         string   `db:"name" json:"name"`
	PasswordHash string   `db:"password_hash" json:"-"`
	Active       bool     `db:"is_active" json:"-"`
	Roles        []string `json:"roles"`
	Permissions  []string `json:"permissions"`
}

func (u User) Can(permission string) bool {
	return slices.Contains(u.Permissions, "*") || slices.Contains(u.Permissions, permission)
}

type Client struct{ IP, UserAgent string }
type NewUser struct{ Username, Name, Password, Role string }

type Repository interface {
	Provision(context.Context, NewUser, string, bool) (bool, error)
	ChangePassword(context.Context, string, string) error
}
type Service struct {
	repo      Repository
	passwords PasswordHasher
}

func NewService(repo Repository, passwords PasswordHasher) *Service {
	return &Service{repo: repo, passwords: passwords}
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9_.-]{1,64}$`)

func (s *Service) Provision(ctx context.Context, input NewUser, initial bool) (bool, error) {
	input.Username = strings.ToLower(strings.TrimSpace(input.Username))
	if input.Name == "" {
		input.Name = input.Username
	}
	if !usernamePattern.MatchString(input.Username) {
		return false, errors.New("用户名只能包含小写字母、数字、下划线、点和连字符，最多 64 个字节。")
	}
	if !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name) > 255 {
		return false, errors.New("显示名称最多 255 个字符。")
	}
	if err := validatePassword(input.Password); err != nil {
		return false, err
	}
	if !slices.Contains([]string{"super_admin", "administrator", "operator", "auditor"}, input.Role) {
		return false, errors.New("无效的角色。")
	}
	hash, err := s.passwords.Hash(input.Password)
	if err != nil {
		return false, err
	}
	return s.repo.Provision(ctx, input, hash, initial)
}

func (s *Service) ChangePassword(ctx context.Context, username, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := s.passwords.Hash(password)
	if err != nil {
		return err
	}
	return s.repo.ChangePassword(ctx, strings.ToLower(strings.TrimSpace(username)), hash)
}

func validatePassword(password string) error {
	if !utf8.ValidString(password) || strings.ContainsRune(password, 0) {
		return errors.New("密码包含无效字符。")
	}
	if len(password) < 5 {
		return errors.New("密码最少 5 个字节。")
	}
	if len(password) > 255 {
		return errors.New("密码最多 255 个字节。")
	}
	return nil
}
