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
	service.RegistrationDefaultGroupEditable = true
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

func TestRegistrationEnvironmentPreservesUnsetAndAuditsStartup(t *testing.T) {
	store := &registrationMemory{group: 99}
	log := &registrationAudit{}
	service := &Management{Registration: store, Audit: log}
	ctx := context.Background()
	if err := service.ApplyRegistrationDefault(ctx, nil); err != nil || store.writes != 0 {
		t.Fatal(err)
	}
	zero := int64(0)
	if err := service.ApplyRegistrationDefault(ctx, &zero); err != nil {
		t.Fatal(err)
	}
	if store.group != 0 || store.writes != 1 || len(log.events) != 2 {
		t.Fatalf("missing policy/audit: %+v %+v", store, log)
	}
	if log.events[1].ActorID != nil || log.events[1].Username != "environment" || log.events[1].Metadata["previous_group_id"] != int64(99) {
		t.Fatal("wrong startup audit")
	}
	if err := service.ApplyRegistrationDefault(ctx, &zero); err != nil || store.writes != 1 {
		t.Fatal("unchanged startup rewrote policy", err)
	}
	bad := int64(2)
	if err := service.ApplyRegistrationDefault(ctx, &bad); err == nil {
		t.Fatal("invalid default accepted")
	}
}

func TestRegistrationStartupDefaultAndEditingAreIndependent(t *testing.T) {
	for _, editable := range []bool{false, true} {
		for _, configured := range []*int64{nil, ptrRegistrationGroup(0), ptrRegistrationGroup(99)} {
			store := &registrationMemory{group: 0}
			service := &Management{Registration: store, Audit: &registrationAudit{}, RegistrationDefaultGroupEditable: editable}
			ctx := context.Background()
			if err := service.ApplyRegistrationDefault(ctx, configured); err != nil {
				t.Fatal(err)
			}
			state, err := service.RegistrationSettings(ctx)
			if err != nil || state.Editable != editable {
				t.Fatalf("unexpected state %+v %v", state, err)
			}
			if configured != nil && state.DefaultGroupID != *configured {
				t.Fatal("startup default not applied")
			}
			target := int64(99) - state.DefaultGroupID
			_, err = service.UpdateRegistration(ctx, RegistrationUpdate{DefaultGroupID: &target, ExpectedGroupID: &state.DefaultGroupID}, identityapi.User{ID: 1, Permissions: []string{"settings.manage"}}, identityapi.Client{})
			if !editable {
				if !errors.Is(err, ErrRegistrationLocked) || store.group != state.DefaultGroupID {
					t.Fatal("locked update accepted", err)
				}
			} else {
				if err != nil || store.group != target {
					t.Fatal("editable update rejected", err)
				}
				if err = service.ApplyRegistrationDefault(ctx, configured); err != nil {
					t.Fatal(err)
				}
				expected := target
				if configured != nil {
					expected = *configured
				}
				if store.group != expected {
					t.Fatal("restart semantics changed")
				}
			}
		}
	}
}

func ptrRegistrationGroup(value int64) *int64 { return &value }
