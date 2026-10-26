package accounts

import (
	"context"
	"errors"

	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
)

var ErrRegistrationLocked = errors.New("注册权限设置已锁定。")

type RegistrationStore interface {
	RegistrationGroup(context.Context) (int64, error)
	SetRegistrationGroup(context.Context, int64, int64) (database.Row, error)
}

type RegistrationSettings struct {
	DefaultGroupID int64 `json:"default_group_id"`
	Editable       bool  `json:"editable"`
}

type RegistrationUpdate struct {
	DefaultGroupID  *int64 `json:"default_group_id"`
	ExpectedGroupID *int64 `json:"expected_group_id"`
}

func (s *Management) RegistrationSettings(ctx context.Context) (RegistrationSettings, error) {
	group, err := s.Registration.RegistrationGroup(ctx)
	return RegistrationSettings{DefaultGroupID: group, Editable: s.RegistrationDefaultGroupEditable}, err
}

func (s *Management) UpdateRegistration(ctx context.Context, input RegistrationUpdate, user identityapi.User, client identityapi.Client) (database.Row, error) {
	if !s.RegistrationDefaultGroupEditable {
		return nil, ErrRegistrationLocked
	}
	if !user.Can("settings.manage") {
		return nil, ErrGMPermission
	}
	validGroup := func(group *int64) bool { return group != nil && (*group == 0 || *group == GMGroupID) }
	if !validGroup(input.DefaultGroupID) || !validGroup(input.ExpectedGroupID) {
		return nil, invalid("registration_group_invalid", "请选择普通玩家或 GM，并提供当前默认权限组。")
	}
	return s.mutate(ctx, "registration.group_changed", map[string]any{"previous_group_id": *input.ExpectedGroupID, "default_group_id": *input.DefaultGroupID}, user, client, func() (database.Row, error) {
		return s.Registration.SetRegistrationGroup(ctx, *input.ExpectedGroupID, *input.DefaultGroupID)
	})
}

func (r *SQLWriter) RegistrationGroup(ctx context.Context) (int64, error) {
	var group int64
	err := r.DB.GetContext(ctx, &group, "SELECT default_group_id FROM happyro_registration_settings WHERE id=1")
	if err != nil {
		return 0, err
	}
	if group != 0 && group != GMGroupID {
		return 0, errors.New("invalid registration group")
	}
	return group, nil
}

func (r *SQLWriter) SetRegistrationGroup(ctx context.Context, expected, group int64) (database.Row, error) {
	tx, err := r.DB.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current int64
	if err = tx.GetContext(ctx, &current, "SELECT default_group_id FROM happyro_registration_settings WHERE id=1 FOR UPDATE"); err != nil {
		return nil, err
	}
	if current != expected {
		return nil, ErrGroupChanged
	}
	if _, err = tx.ExecContext(ctx, "UPDATE happyro_registration_settings SET default_group_id=? WHERE id=1", group); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return database.Row{"default_group_id": group}, nil
}

// ApplyRegistrationDefault applies an explicitly configured deployment default at
// startup. An unset value preserves the saved policy. Administrator changes are
// controlled independently by the edit switch.
func (s *Management) ApplyRegistrationDefault(ctx context.Context, group *int64) error {
	if group == nil {
		return nil
	}
	if *group != 0 && *group != GMGroupID {
		return errors.New("invalid registration group")
	}
	current, err := s.Registration.RegistrationGroup(ctx)
	if err != nil {
		return err
	}
	if current == *group {
		return nil
	}
	_, err = s.mutateEvent(ctx, "registration.group_changed", map[string]any{"previous_group_id": current, "default_group_id": *group, "source": "environment"}, nil, "environment", identityapi.Client{}, func() (database.Row, error) {
		return s.Registration.SetRegistrationGroup(ctx, current, *group)
	})
	return err
}
