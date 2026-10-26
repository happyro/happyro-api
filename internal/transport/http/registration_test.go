package httptransport

import (
	"context"
	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/platform/database"
	"strings"
	"testing"
)

type registrationStore struct {
	group  int64
	writes int
}

func (r *registrationStore) RegistrationGroup(context.Context) (int64, error) { return r.group, nil }
func (r *registrationStore) SetRegistrationGroup(_ context.Context, expected, group int64) (database.Row, error) {
	if expected != r.group {
		return nil, accountsapi.ErrGroupChanged
	}
	r.group = group
	r.writes++
	return database.Row{"default_group_id": group}, nil
}
func TestRegistrationSettingsLockCannotBeBypassedWithDirectRequests(t *testing.T) {
	store := &registrationStore{group: 99}
	management := &accountsapi.Management{Registration: store, Audit: playerAudit{}}
	b, repo := newBrowser(t, func(s *Server) { s.AccountManagement = management })
	path := "/api/settings/registration"
	expectStatus(t, b.request("GET", path, "", "", false), 401)
	b.login(t)
	read := b.request("GET", path, "", "", false)
	expectStatus(t, read, 200)
	if !strings.Contains(read.Body.String(), `"editable":false`) || !strings.Contains(read.Body.String(), `"default_group_id":99`) {
		t.Fatal(read.Body.String())
	}
	body := `{"default_group_id":0,"expected_group_id":99}`
	expectStatus(t, b.request("PUT", path, body, "", false), 419)
	locked := b.request("PUT", path, body, "", true)
	expectStatus(t, locked, 403)
	if !strings.Contains(locked.Body.String(), "registration_settings_locked") || store.writes != 0 {
		t.Fatal(locked.Body.String())
	}
	management.RegistrationEditable = true
	expectStatus(t, b.request("PUT", path, body, "", true), 200)
	expectStatus(t, b.request("PUT", path, body, "", true), 409)
	expectStatus(t, b.request("PUT", path, `{"default_group_id":100,"expected_group_id":0}`, "", true), 422)
	repo.user.Permissions = []string{"players.view"}
	expectStatus(t, b.request("PUT", path, body, "", true), 403)
	expectStatus(t, b.request("GET", path, "", "", false), 403)
}
