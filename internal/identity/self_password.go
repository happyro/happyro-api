package identity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
)

var ErrCurrentPassword = errors.New("当前密码不正确或已变更，请重新登录后重试。")
var ErrPasswordInput = errors.New("密码输入无效。")

type PasswordRateLimited struct{ RetryAfter int }

func (e *PasswordRateLimited) Error() string {
	return "修改密码尝试次数过多，请稍后再试。"
}

type SelfPasswordRepository interface {
	ByID(context.Context, int64) (User, error)
	Attempt(context.Context, string) (int, error)
	ReplacePassword(context.Context, User, string, Client) error
}
type SelfPasswordService struct {
	repo      SelfPasswordRepository
	passwords PasswordHasher
}

func NewSelfPasswordService(repo SelfPasswordRepository, passwords PasswordHasher) *SelfPasswordService {
	return &SelfPasswordService{repo: repo, passwords: passwords}
}
func (s *SelfPasswordService) Change(ctx context.Context, id int64, current, next, confirmation string, client Client) error {
	if len(current) > 255 {
		return ErrCurrentPassword
	}
	if err := validatePassword(next); err != nil {
		return fmt.Errorf("%w %s", ErrPasswordInput, err)
	}
	if next != confirmation {
		return fmt.Errorf("%w 两次输入的新密码不一致。", ErrPasswordInput)
	}
	if current == next {
		return fmt.Errorf("%w 新密码不能与当前密码相同。", ErrPasswordInput)
	}
	key := sha256.Sum256([]byte(fmt.Sprintf("admin-password:%d", id)))
	retry, err := s.repo.Attempt(ctx, hex.EncodeToString(key[:]))
	if err != nil {
		return err
	}
	if retry > 0 {
		return &PasswordRateLimited{retry}
	}
	user, err := s.repo.ByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return ErrCurrentPassword
	}
	if err != nil {
		return err
	}
	if !user.Active || !s.passwords.Verify(current, user.PasswordHash) {
		return ErrCurrentPassword
	}
	hash, err := s.passwords.Hash(next)
	if err != nil {
		return err
	}
	return s.repo.ReplacePassword(ctx, user, hash, client)
}
