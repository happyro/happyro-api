package settings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	gameapi "github.com/happyro/happyro-api/internal/game"
)

var ErrUnconfirmed = errors.New("游戏设置尚未确认生效，请在修改记录中继续确认。")
var ErrFileChanged = errors.New("游戏配置文件已被其他操作修改，请先核对配置。")

type Publication interface {
	Read() (string, error)
	Publish(string) error
}
type Commands interface {
	Submit(context.Context, gameapi.Request, gameapi.Actor) (gameapi.Command, bool, error)
	ResolveSettings(context.Context, string, bool) (gameapi.Command, error)
}
type RevisionSession interface {
	Find(context.Context, string) (*Revision, error)
	Create(context.Context, string, string, map[string]int64, string, string, gameapi.Actor) (Revision, error)
	Transition(context.Context, string, string, string, *string) error
	AdvanceAttempt(context.Context, string, uint32) error
	SaveAdmission(context.Context, string, uint32, string, uint64) error
	Close() error
}
type RevisionStore interface {
	Acquire(context.Context) (RevisionSession, error)
}

func (r *SQLRevisions) Acquire(ctx context.Context) (RevisionSession, error) { return r.Lock(ctx) }

type SettingsGateway interface {
	BattleConfig(context.Context) (map[string]int64, error)
	SettingsState(context.Context) (gameapi.SettingsState, error)
	FenceSettings(context.Context, string, uint64) error
}

type Service struct {
	Revisions RevisionStore
	File      Publication
	Commands  Commands
	Gateway   SettingsGateway
}
type Update struct {
	Key     string           `json:"idempotency_key"`
	Changes map[string]int64 `json:"changes"`
}

func (s *Service) Apply(ctx context.Context, input Update, actor gameapi.Actor) (Revision, error) {
	return s.apply(ctx, input, actor, false)
}

// Resume is called only by the administrator-authorized recovery use case.
func (s *Service) Resume(ctx context.Context, input Update, actor gameapi.Actor) (Revision, error) {
	return s.apply(ctx, input, actor, true)
}

func (s *Service) apply(ctx context.Context, input Update, actor gameapi.Actor, retryRejected bool) (result Revision, err error) {
	if !actor.Valid() {
		return result, errors.New("invalid settings actor")
	}
	if len(input.Key) < 1 || len(input.Key) > 64 || !utf8.ValidString(input.Key) || strings.TrimSpace(input.Key) != input.Key || strings.ContainsRune(input.Key, 0) {
		return result, &gameapi.ValidationError{Field: "idempotency_key", Message: "请求标识必须包含 1 至 64 个字节。"}
	}
	if err = Validate(input.Changes, actor.GameAccountID != nil); err != nil {
		return result, err
	}
	state, stateErr := s.Gateway.SettingsState(ctx)
	if stateErr != nil {
		return result, stateErr
	}
	for key := range input.Changes {
		if name, locked := state.EnvironmentOverrides[key]; locked {
			return result, &gameapi.ValidationError{Field: key, Message: fmt.Sprintf("该设置由环境变量 %s 管理，请修改部署配置并重启地图服；清空该变量后可由后台管理。", name)}
		}
	}

	raw, err := json.Marshal(struct {
		Changes map[string]int64
		Actor   gameapi.Actor
	}{input.Changes, actor})
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	session, err := s.Revisions.Acquire(ctx)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	row, err := session.Find(ctx, input.Key)
	if err != nil {
		return result, err
	}
	if row != nil {
		if row.Hash != hash {
			return result, gameapi.ErrConflict
		}
		if row.Status == "applied" {
			return *row, nil
		}
		if row.Status == "failed" {
			return *row, ErrUnconfirmed
		}
	} else {
		previous, readErr := s.File.Read()
		if readErr != nil {
			return result, readErr
		}
		desired, renderErr := Render(previous, input.Changes)
		if renderErr != nil {
			return result, renderErr
		}
		created, createErr := session.Create(ctx, input.Key, hash, input.Changes, previous, desired, actor)
		if createErr != nil {
			return result, createErr
		}
		row = &created
	}
	current, err := s.File.Read()
	if err != nil {
		return *row, err
	}
	if row.Status == "prepared" {
		if current != row.Previous && current != row.Desired {
			return *row, ErrFileChanged
		}
		// Publish again after an interrupted write even when bytes match, ensuring
		// durability before recording publication. This does not repeat a game command.
		if err = s.File.Publish(row.Desired); err != nil {
			return *row, err
		}
		if err = session.Transition(ctx, row.ID, "prepared", "published", nil); err != nil {
			return *row, err
		}
		row.Status = "published"
	} else if current != row.Desired {
		return *row, ErrFileChanged
	}
	keys := make([]string, 0, len(input.Changes))
	for key := range input.Changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	changes := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		changes = append(changes, map[string]any{"key": key, "value": input.Changes[key]})
	}
	if err = s.submit(ctx, session, row, changes, actor, retryRejected); err != nil {
		return *row, err
	}
	actual, err := s.Gateway.BattleConfig(ctx)
	if err != nil {
		return *row, err
	}
	for key, value := range input.Changes {
		if got, ok := actual[key]; !ok || got != value {
			return *row, ErrUnconfirmed
		}
	}
	if err = session.Transition(ctx, row.ID, row.Status, "applied", nil); err != nil {
		return *row, err
	}
	updated, err := session.Find(ctx, input.Key)
	if err != nil {
		return *row, err
	}
	return *updated, nil
}

func (s *Service) submit(ctx context.Context, session RevisionSession, row *Revision, changes []map[string]any, actor gameapi.Actor, retryRejected bool) error {
	if err := s.admit(ctx, session, row); err != nil {
		return err
	}
	request := gameapi.Request{IdempotencyKey: fmt.Sprintf("settings:%s:%d", row.ID, row.Attempt), Type: "battle_config.apply", Target: &gameapi.Target{Type: "server", ID: "primary"}, Payload: map[string]any{"changes": changes, "instance_id": *row.MapInstance, "expected_revision": *row.MapRevision}}
	command, _, err := s.Commands.Submit(ctx, request, actor)
	if retryRejected && command.Status == "indeterminate" {
		command, err = s.reconcile(ctx, row, command)
	}
	if err == nil || !retryRejected || command.Status != "failed" {
		return err
	}
	var failure *gameapi.GatewayError
	if !errors.As(err, &failure) || failure.OutcomeUnknown {
		return err
	}
	if err = session.AdvanceAttempt(ctx, row.ID, row.Attempt); err != nil {
		return err
	}
	row.Attempt++
	row.MapInstance, row.MapRevision = nil, nil
	if err = s.admit(ctx, session, row); err != nil {
		return err
	}
	request.Payload["instance_id"] = *row.MapInstance
	request.Payload["expected_revision"] = *row.MapRevision
	request.IdempotencyKey = fmt.Sprintf("settings:%s:%d", row.ID, row.Attempt)
	// Persist the attempt before submitting. After interruption, resume uses this
	// identity again; the original rejection remains immutable command history.
	_, _, err = s.Commands.Submit(ctx, request, actor)
	return err
}

func (s *Service) admit(ctx context.Context, session RevisionSession, row *Revision) error {
	if row.MapInstance != nil && row.MapRevision != nil {
		return nil
	}
	if row.MapInstance != nil || row.MapRevision != nil {
		return ErrUnconfirmed
	}
	state, err := s.Gateway.SettingsState(ctx)
	if err != nil {
		return err
	}
	if err = session.SaveAdmission(ctx, row.ID, row.Attempt, state.InstanceID, state.Revision); err != nil {
		return err
	}
	row.MapInstance, row.MapRevision = &state.InstanceID, &state.Revision
	return nil
}
