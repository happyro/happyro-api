package accounts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/happyro/happyro-api/internal/audit"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
)

var ErrAccountNotFound = errors.New("账号不存在。")
var ErrAccountExists = errors.New("账号已存在。")
var ErrAccountDeleting = errors.New("账号已进入删除流程，无法修改。")
var ErrProtected = errors.New("不能通过玩家管理修改服务器账号。")

type ValidationError struct{ Code, Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(code, message string) error { return &ValidationError{code, message} }

type NewAccount struct {
	Username string `json:"userid"`
	Password string `json:"user_pass"`
	Email    string `json:"email"`
	Sex      string `json:"sex"`
}
type AccountUpdate struct {
	Email *string `json:"email"`
	State *int64  `json:"state"`
}
type AccountWriter interface {
	Create(context.Context, NewAccount) (database.Row, error)
	Update(context.Context, int64, AccountUpdate) (database.Row, error)
	Password(context.Context, int64, string) (database.Row, error)
}
type Management struct {
	GM       GMWriter
	Accounts AccountWriter
	Audit    audit.Writer
}

// Match HappyRO client RegistrationValidation.js and PACKETVER=20211103.
func credential(value, label, code string, maximum int) error {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return invalid(code+"_invalid", label+"包含无效字符。")
	}
	if len(value) < 6 {
		return invalid(code+"_too_short", label+"最少 6 个字节")
	}
	if len(value) > maximum {
		return invalid(code+"_too_long", fmt.Sprintf("%s最多 %d 个字节", label, maximum))
	}
	return nil
}
func validEmail(value string) error {
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value || len(value) > 39 || strings.IndexFunc(value, func(r rune) bool { return r > 127 || unicode.IsControl(r) }) >= 0 {
		return invalid("email_invalid", "请输入有效的邮箱地址，最多 39 个字节。")
	}
	return nil
}
func validAccountID(id int64) error {
	if id < 1 || id > math.MaxInt32 {
		return ErrAccountNotFound
	}
	return nil
}
func (s *Management) Create(ctx context.Context, input NewAccount, user identityapi.User, client identityapi.Client) (database.Row, error) {
	if err := credential(input.Username, "账号", "account", 21); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Username) != input.Username {
		return nil, invalid("account_invalid", "账号首尾不能包含空格。")
	}
	lowerName := strings.ToLower(input.Username)
	if strings.HasSuffix(lowerName, "_m") || strings.HasSuffix(lowerName, "_f") {
		return nil, invalid("account_invalid", "账号不能以 _M 或 _F 结尾。")
	}
	if err := credential(input.Password, "密码", "password", 23); err != nil {
		return nil, err
	}
	if err := validEmail(input.Email); err != nil {
		return nil, err
	}
	if input.Sex != "M" && input.Sex != "F" {
		return nil, invalid("sex_invalid", "请选择男性或女性。")
	}
	return s.mutate(ctx, "player.registered", map[string]any{"username": input.Username}, user, client, func() (database.Row, error) { return s.Accounts.Create(ctx, input) })
}
func (s *Management) Update(ctx context.Context, id int64, input AccountUpdate, user identityapi.User, client identityapi.Client) (database.Row, error) {
	if err := validAccountID(id); err != nil {
		return nil, err
	}
	fields := []string{}
	if input.Email != nil {
		if err := validEmail(*input.Email); err != nil {
			return nil, err
		}
		fields = append(fields, "email")
	}
	if input.State != nil {
		if *input.State < 0 || *input.State > math.MaxUint32 {
			return nil, invalid("state_invalid", "账号状态超出有效范围。")
		}
		fields = append(fields, "state")
	}
	if len(fields) == 0 {
		return nil, invalid("empty_update", "请提供要修改的字段。")
	}
	return s.mutate(ctx, "player.updated", map[string]any{"account_id": id, "fields": fields}, user, client, func() (database.Row, error) { return s.Accounts.Update(ctx, id, input) })
}
func (s *Management) Password(ctx context.Context, id int64, password string, user identityapi.User, client identityapi.Client) (database.Row, error) {
	if err := validAccountID(id); err != nil {
		return nil, err
	}
	if err := credential(password, "密码", "password", 23); err != nil {
		return nil, err
	}
	return s.mutate(ctx, "player.password_reset", map[string]any{"account_id": id}, user, client, func() (database.Row, error) { return s.Accounts.Password(ctx, id, password) })
}
func (s *Management) mutate(ctx context.Context, action string, metadata map[string]any, user identityapi.User, client identityapi.Client, run func() (database.Row, error)) (database.Row, error) {
	if user.ID <= 0 {
		return nil, errors.New("invalid admin actor")
	}
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return nil, err
	}
	operation := "player:" + hex.EncodeToString(token)
	event := audit.Event{OperationKey: operation + ":requested", ActorID: &user.ID, Username: user.Username, Action: action + ".requested", IP: client.IP, UserAgent: client.UserAgent, Metadata: metadata}
	// Admin and game databases cannot commit atomically, and login is MyISAM.
	// Persist intent before writing, then a result; an interrupted operation stays
	// visible as an unresolved intent instead of losing its audit trail.
	if err := s.Audit.Write(ctx, event); err != nil {
		return nil, err
	}
	row, writeErr := run()
	event.OperationKey = operation + ":result"
	event.Action = action
	if writeErr != nil {
		event.Action += ".unconfirmed"
	} else if row != nil {
		metadata["account_id"] = row["account_id"]
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	auditErr := s.Audit.Write(persist, event)
	if writeErr != nil {
		return nil, writeErr
	}
	if auditErr != nil {
		return nil, auditErr
	}
	return row, nil
}
