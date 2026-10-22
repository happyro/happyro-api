package game

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrConflict = errors.New("幂等键已用于其他请求。")
	ErrNotFound = errors.New("游戏命令不存在。")
	ErrRunning  = errors.New("游戏命令仍在执行，不能重复提交。")
)

type GatewayError struct {
	Code           string
	Message        string
	OutcomeUnknown bool
}

func (e *GatewayError) Error() string { return e.Message }

type Actor struct {
	AdminUserID   *int64 `json:"requested_by"`
	GameAccountID *int64 `json:"requested_game_account_id"`
}

func (a Actor) Valid() bool {
	return (a.AdminUserID != nil && *a.AdminUserID > 0 && a.GameAccountID == nil) || (a.GameAccountID != nil && *a.GameAccountID > 0 && a.AdminUserID == nil)
}

type Target struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}
type Request struct {
	IdempotencyKey string         `json:"idempotency_key"`
	Type           string         `json:"type"`
	Target         *Target        `json:"target"`
	Payload        map[string]any `json:"payload"`
}

func (r Request) Fingerprint(actor Actor) (string, error) {
	raw, err := json.Marshal(struct {
		Request Request
		Actor   Actor
	}{r, actor})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type Command struct {
	ID                     string         `json:"id"`
	IdempotencyKey         string         `json:"idempotencyKey"`
	Type                   string         `json:"type"`
	Status                 string         `json:"status"`
	TargetType             *string        `json:"targetType"`
	TargetID               *string        `json:"targetId"`
	Payload                map[string]any `json:"payload"`
	Result                 map[string]any `json:"result"`
	ErrorCode              *string        `json:"errorCode"`
	ErrorMessage           *string        `json:"errorMessage"`
	RequestedBy            *int64         `json:"requestedBy"`
	RequestedGameAccountID *int64         `json:"requestedGameAccountId"`
	StartedAt              *time.Time     `json:"startedAt"`
	CompletedAt            *time.Time     `json:"completedAt"`
	CreatedAt              time.Time      `json:"createdAt"`
	UpdatedAt              time.Time      `json:"updatedAt"`
}

func (c Command) Target() *Target {
	if c.TargetType == nil || c.TargetID == nil {
		return nil
	}
	return &Target{*c.TargetType, *c.TargetID}
}

type Capabilities struct {
	ProtocolVersion string   `json:"protocol_version"`
	Commands        []string `json:"commands"`
}
type Gateway interface {
	Capabilities(context.Context) (Capabilities, error)
	Execute(context.Context, Command) (map[string]any, error)
	BattleConfig(context.Context) (map[string]int64, error)
	CharacterSnapshot(context.Context, int64) (map[string]any, error)
}
type Repository interface {
	Submit(context.Context, Request, Actor) (Command, bool, error)
	Find(context.Context, string) (Command, error)
	Claim(context.Context, string, time.Duration) (Command, error)
	Finish(context.Context, string, string, map[string]any, *GatewayError) (Command, error)
}

func NewID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
