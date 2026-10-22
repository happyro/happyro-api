package rewards

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/happyro/happyro-api/internal/audit"
	"github.com/happyro/happyro-api/internal/catalog"
	gameapi "github.com/happyro/happyro-api/internal/game"
	identityapi "github.com/happyro/happyro-api/internal/identity"
)

var ErrConflict = errors.New("该请求标识已用于其他物品发放。")
var ErrAccountDeleting = errors.New("角色所属账号已进入删除流程，无法发放。")
var ErrCharacter = errors.New("角色不存在。")
var ErrPending = errors.New("邮件发放结果待确认，请使用原请求标识重试。")

type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(message string) error       { return &ValidationError{message} }

type ItemRequest struct {
	Key         string `json:"idempotency_key"`
	ItemID      int64  `json:"item_id"`
	CharacterID int64  `json:"char_id"`
	Amount      int64  `json:"amount"`
	Title       string `json:"title"`
	Message     string `json:"message"`
	Bound       bool   `json:"bound"`
	Delivery    string `json:"delivery"`
}
type ZenyRequest struct {
	Key         string `json:"idempotency_key"`
	CharacterID int64  `json:"char_id"`
	Amount      int64  `json:"amount"`
}
type PreparedMail struct {
	Request           ItemRequest
	Sender            string
	AttachmentAmounts []int64
}
type MailRecord struct {
	ID           int64
	Hash, Status string
	MailID       *int64
	Prepared     PreparedMail
}
type MailRecords interface {
	Find(context.Context, string) (*MailRecord, error)
	Create(context.Context, string, string, PreparedMail, int64) (MailRecord, error)
	Sent(context.Context, MailRecord, int64, identityapi.User, identityapi.Client) error
}
type MailDelivery interface {
	Deliver(context.Context, string, string, PreparedMail) (int64, error)
}
type ItemReader interface {
	Item(context.Context, int64, string) (catalog.Object, error)
}
type CommandService interface {
	Submit(context.Context, gameapi.Request, gameapi.Actor) (gameapi.Command, bool, error)
}
type Service struct {
	Items    ItemReader
	Records  MailRecords
	Mail     MailDelivery
	Commands CommandService
	Audit    audit.Writer
}

func validKey(key string) bool {
	return len(key) > 0 && len(key) <= 64 && strings.TrimSpace(key) == key && utf8.ValidString(key) && !strings.ContainsRune(key, 0)
}
func validateItem(input ItemRequest) error {
	if !validKey(input.Key) {
		return invalid("请求标识必须包含 1 至 64 个字节。")
	}
	if input.ItemID <= 0 || input.ItemID > math.MaxInt32 || input.CharacterID <= 0 || input.CharacterID > math.MaxInt32 {
		return invalid("物品或角色编号无效。")
	}
	if input.Amount < 1 || input.Amount > 30000 {
		return invalid("物品数量必须是 1 至 30000。")
	}
	return nil
}
func MailText(value, label string, maximum int, required bool) error {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return invalid(label + "包含无效字符。")
	}
	if required && strings.TrimSpace(value) == "" {
		return invalid("请输入" + label + "。")
	}
	if len(value) > maximum {
		return invalid(fmt.Sprintf("%s最多 %d 个 UTF-8 字节（汉字通常占 3 字节）。", label, maximum))
	}
	return nil
}
func fingerprint(input ItemRequest, actorID int64) (string, error) {
	raw, err := json.Marshal(struct {
		Request ItemRequest
		Actor   int64
	}{input, actorID})
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}
func (s *Service) MailItem(ctx context.Context, input ItemRequest, user identityapi.User, client identityapi.Client) (int64, error) {
	if err := validateItem(input); err != nil {
		return 0, err
	}
	if user.ID <= 0 {
		return 0, errors.New("invalid admin actor")
	}
	input.Delivery = "mail"
	for _, field := range []struct {
		value, label string
		max          int
	}{{input.Title, "邮件标题", 39}, {input.Message, "邮件内容", 499}} {
		if err := MailText(field.value, field.label, field.max, true); err != nil {
			return 0, err
		}
	}
	hash, err := fingerprint(input, user.ID)
	if err != nil {
		return 0, err
	}
	existing, err := s.Records.Find(ctx, input.Key)
	if err != nil {
		return 0, err
	}
	var record MailRecord
	if existing != nil {
		record = *existing
	} else {
		if err = MailText(user.Username, "发件人名称", 23, true); err != nil {
			return 0, err
		}
		item, err := s.Items.Item(ctx, input.ItemID, "server")
		if err != nil {
			return 0, err
		}
		kind, _ := item["Type"].(string)
		if kind == "PetEgg" && input.Amount != 1 {
			return 0, invalid("宠物蛋邮件每次只能发放 1 个。")
		}
		separate := slices.Contains([]string{"Weapon", "Armor", "PetEgg", "PetArmor", "ShadowGear"}, kind)
		if flags, ok := item["Flags"].(map[string]any); ok && flags["UniqueId"] == true {
			separate = true
		}
		if separate && input.Amount > 5 {
			return 0, invalid("此物品每封邮件最多发放 5 件，请分批发放。")
		}
		amounts := []int64{input.Amount}
		if separate {
			amounts = make([]int64, input.Amount)
			for i := range amounts {
				amounts[i] = 1
			}
		}
		record, err = s.Records.Create(ctx, input.Key, hash, PreparedMail{Request: input, Sender: user.Username, AttachmentAmounts: amounts}, user.ID)
		if err != nil {
			return 0, err
		}
	}
	if record.Hash != hash {
		return 0, ErrConflict
	}
	if record.Status == "sent" && record.MailID != nil {
		return *record.MailID, nil
	}
	mailID, err := s.Mail.Deliver(ctx, input.Key, hash, record.Prepared)
	if err != nil {
		return 0, err
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = s.Records.Sent(persist, record, mailID, user, client); err != nil {
		return 0, err
	}
	return mailID, nil
}
func (s *Service) Inventory(ctx context.Context, input ItemRequest, actor gameapi.Actor, client identityapi.Client) (map[string]any, error) {
	if err := validateItem(input); err != nil {
		return nil, err
	}
	if input.Bound {
		return nil, invalid("背包发放暂不支持绑定物品，请使用邮件发放。")
	}
	item, err := s.Items.Item(ctx, input.ItemID, "server")
	if err != nil {
		return nil, err
	}
	kind, _ := item["Type"].(string)
	if kind == "PetEgg" {
		return nil, invalid("宠物蛋仅支持邮件发放，每次 1 个。")
	}
	payload := map[string]any{"item_id": input.ItemID, "amount": input.Amount, "identify": slices.Contains([]string{"Weapon", "Armor", "PetArmor", "ShadowGear"}, kind)}
	return s.command(ctx, input.Key, input.CharacterID, "character.inventory.item_grant", payload, actor, client)
}
func (s *Service) Zeny(ctx context.Context, input ZenyRequest, actor gameapi.Actor, client identityapi.Client) (map[string]any, error) {
	if !validKey(input.Key) || input.CharacterID <= 0 || input.CharacterID > math.MaxInt32 {
		return nil, invalid("请求标识或角色编号无效。")
	}
	if input.Amount < 1 || input.Amount > math.MaxInt32 {
		return nil, invalid("Zeny 数量必须是 1 至 2147483647。")
	}
	return s.command(ctx, input.Key, input.CharacterID, "character.currency.zeny_grant", map[string]any{"amount": input.Amount}, actor, client)
}
func (s *Service) command(ctx context.Context, key string, id int64, kind string, payload map[string]any, actor gameapi.Actor, client identityapi.Client) (map[string]any, error) {
	command, _, err := s.Commands.Submit(ctx, gameapi.Request{IdempotencyKey: key, Type: kind, Target: &gameapi.Target{Type: "character", ID: strconv.FormatInt(id, 10)}, Payload: payload}, actor)
	if err != nil {
		return nil, err
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = s.Audit.Write(persist, audit.Event{OperationKey: "command:" + command.ID, ActorID: actor.AdminUserID, Action: "operations." + kind, IP: client.IP, UserAgent: client.UserAgent, Metadata: map[string]any{"command_id": command.ID, "game_account_id": actor.GameAccountID, "char_id": id, "payload": payload}})
	if err != nil {
		return nil, err
	}
	return command.Result, nil
}
