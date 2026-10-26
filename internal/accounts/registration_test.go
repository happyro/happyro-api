package accounts

import (
	"context"
	"errors"
	"testing"

	"github.com/happyro/happyro-api/internal/audit"
	identityapi "github.com/happyro/happyro-api/internal/identity"
	"github.com/happyro/happyro-api/internal/platform/database"
)

type registrationMemory struct {
	group  int64
	writes int
}

func (m *registrationMemory) RegistrationGroup(context.Context) (int64, error) { return m.group, nil }
func (m *registrationMemory) SetRegistrationGroup(_ context.Context, expected, group int64) (database.Row, error) {
	if expected != m.group {
		return nil, ErrGroupChanged
	}
	m.group = group
	m.writes++
	return database.Row{"default_group_id": group}, nil
}

type registrationAudit struct{ events []audit.Event }

func (a *registrationAudit) Write(_ context.Context, e audit.Event) error {
	a.events = append(a.events, e)
	return nil
}

func TestRegistrationLockAndValidatedUpdates(t *testing.T) {
	ctx := context.Background()
	store := &registrationMemory{group: 99}
	log := &registrationAudit{}
	service := &Management{Registration: store, Audit: log}
	user := identityapi.User{ID: 1, Permissions: []string{"settings.manage"}}
	zero, gm, bad := int64(0), int64(99), int64(100)
	update := RegistrationUpdate{DefaultGroupID: &zero, ExpectedGroupID: &gm}
	got, err := service.RegistrationSettings(ctx)
	if err != nil || got.Editable || got.DefaultGroupID != 99 {
		t.Fatalf("locked state: %+v %v", got, err)
	}
	if _, err = service.UpdateRegistration(ctx, update, user, identityapi.Client{}); !errors.Is(err, ErrRegistrationLocked) {
		t.Fatal(err)
	}
	if store.writes != 0 || len(log.events) != 0 {
		t.Fatal("locked update reached persistence")
	}
	service.RegistrationEditable = true
	for _, input := range []RegistrationUpdate{{}, {DefaultGroupID: &bad, ExpectedGroupID: &gm}} {
		if _, err = service.UpdateRegistration(ctx, input, user, identityapi.Client{}); err == nil {
			t.Fatal("invalid group accepted")
		}
	}
	if _, err = service.UpdateRegistration(ctx, update, identityapi.User{ID: 2}, identityapi.Client{}); !errors.Is(err, ErrGMPermission) {
		t.Fatal(err)
	}
	if _, err = service.UpdateRegistration(ctx, update, user, identityapi.Client{}); err != nil {
		t.Fatal(err)
	}
	if store.group != 0 || store.writes != 1 || len(log.events) != 2 {
		t.Fatalf("missing write/audit: %+v %+v", store, log)
	}
	if _, err = service.UpdateRegistration(ctx, update, user, identityapi.Client{}); !errors.Is(err, ErrGroupChanged) {
		t.Fatal("stale update accepted", err)
	}
}
