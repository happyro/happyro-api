package httptransport

import (
	"context"
	"strings"
	"testing"

	accountsapi "github.com/happyro/happyro-api/internal/accounts"
	"github.com/happyro/happyro-api/internal/audit"
	"github.com/happyro/happyro-api/internal/platform/database"
)

type playerWriter struct{ creates, updates, passwords int }

func (w *playerWriter) Create(_ context.Context, input accountsapi.NewAccount) (database.Row, error) {
	w.creates++
	return database.Row{"account_id": 2000000, "username": input.Username}, nil
}
func (w *playerWriter) Update(context.Context, int64, accountsapi.AccountUpdate) (database.Row, error) {
	w.updates++
	return database.Row{"account_id": 2000000}, nil
}
func (w *playerWriter) Password(context.Context, int64, string) (database.Row, error) {
	w.passwords++
	return database.Row{"account_id": 2000000}, nil
}

type playerAudit struct{}

func (playerAudit) Write(context.Context, audit.Event) error { return nil }

func TestPlayerWriteHTTPUsesValidatedInputsAndSafeProjection(t *testing.T) {
	writer := &playerWriter{}
	b, _ := newBrowser(t, func(s *Server) { s.AccountManagement = &accountsapi.Management{Accounts: writer, Audit: playerAudit{}} })
	b.login(t)
	valid := `{"userid":"player_one","user_pass":"secret","email":"one@example.com","sex":"M"}`
	expectStatus(t, b.request("POST", "/api/players/accounts", valid, "", false), 419)
	response := b.request("POST", "/api/players/accounts", valid, "", true)
	expectStatus(t, response, 201)
	if response.Body.String() != `{"data":{"account_id":2000000,"username":"player_one"}}` {
		t.Fatalf("unexpected response %s", response.Body.String())
	}
	for _, input := range []string{
		`{"userid":"short","user_pass":"secret","email":"one@example.com","sex":"M"}`,
		`{"userid":"player_M","user_pass":"secret","email":"one@example.com","sex":"M"}`,
		`{"userid":"player_one","user_pass":"secret","email":"one@example.com","sex":"S"}`,
		`{"userid":"player_one","user_pass":"secret","email":"one@example.com","sex":"M","group_id":99}`,
	} {
		expectStatus(t, b.request("POST", "/api/players/accounts", input, "", true), 422)
	}
	expectStatus(t, b.request("PATCH", "/api/players/accounts/2000000", `{"email":"updated@example.com","state":0}`, "", true), 200)
	expectStatus(t, b.request("PATCH", "/api/players/accounts/2000000", `{"state":-1}`, "", true), 422)
	expectStatus(t, b.request("PATCH", "/api/players/accounts/2000000", `{"group_id":99}`, "", true), 422)
	expectStatus(t, b.request("PATCH", "/api/players/accounts/2000000", `{}`, "", true), 422)
	expectStatus(t, b.request("POST", "/api/players/accounts/2000000/password", `{"password":"new-secret"}`, "", true), 200)
	expectStatus(t, b.request("POST", "/api/players/accounts/2000000/password", `{"password":"short"}`, "", true), 422)
	expectStatus(t, b.request("POST", "/api/players/accounts/invalid/password", `{"password":"new-secret"}`, "", true), 404)
	if writer.creates != 1 || writer.updates != 1 || writer.passwords != 1 {
		t.Fatalf("invalid request reached writer %+v", writer)
	}
}

type deletionStore struct {
	accountsapi.DeletionStore
	row            accountsapi.Deletion
	lists, retries int
}

func (s *deletionStore) List(context.Context, database.Pagination) (accountsapi.DeletionPage, error) {
	s.lists++
	return accountsapi.DeletionPage{Data: []accountsapi.Deletion{s.row}, Total: 1}, nil
}
func (s *deletionStore) Find(context.Context, int64) (*accountsapi.Deletion, error) {
	return &s.row, nil
}
func (s *deletionStore) Ready(context.Context) error { return nil }
func (s *deletionStore) Retry(context.Context, int64) (accountsapi.Deletion, error) {
	if s.row.Status != "failed" {
		return accountsapi.Deletion{}, accountsapi.ErrDeletionState
	}
	s.retries++
	s.row.Status = "queued"
	return s.row, nil
}
func TestDeletionHTTPShowsPersistedStateAndOnlyRetriesFailedJobs(t *testing.T) {
	store := &deletionStore{row: accountsapi.Deletion{AccountID: 2000000, AccountName: "player_one", Status: "failed"}}
	b, _ := newBrowser(t, func(s *Server) { s.AccountDeletions = &accountsapi.Deletions{Store: store, Audit: playerAudit{}} })
	expectStatus(t, b.request("GET", "/api/players/account-deletions", "", "", false), 401)
	b.login(t)
	expectStatus(t, b.request("GET", "/api/players/account-deletions?page=0", "", "", false), 422)
	expectStatus(t, b.request("GET", "/api/players/account-deletions?perPage=101", "", "", false), 422)
	response := b.request("GET", "/api/players/account-deletions?page=1&perPage=10", "", "", false)
	expectStatus(t, response, 200)
	if !strings.Contains(response.Body.String(), `"status":"failed"`) || store.lists != 1 {
		t.Fatalf("list: %s", response.Body.String())
	}
	expectStatus(t, b.request("POST", "/api/players/accounts/2000000/deletion/retry", "{}", "", false), 419)
	expectStatus(t, b.request("POST", "/api/players/accounts/2000000/deletion/retry", "{}", "", true), 202)
	expectStatus(t, b.request("POST", "/api/players/accounts/2000000/deletion/retry", "{}", "", true), 409)
	response = b.request("GET", "/api/players/accounts/2000000/deletion", "", "", false)
	expectStatus(t, response, 200)
	if !strings.Contains(response.Body.String(), `"status":"queued"`) || store.retries != 1 {
		t.Fatalf("retry: %s", response.Body.String())
	}
}

type restrictedPlayerWriter struct {
	accountsapi.AccountWriter
	failure error
}

func (w restrictedPlayerWriter) Update(context.Context, int64, accountsapi.AccountUpdate) (database.Row, error) {
	return nil, w.failure
}
func TestAccountUpdateReportsDeletionAndLockConflicts(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{accountsapi.ErrAccountDeleting, "account_deleting"},
		{database.ErrAccountBusy, "account_busy"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			b, _ := newBrowser(t, func(s *Server) {
				s.AccountManagement = &accountsapi.Management{Accounts: restrictedPlayerWriter{failure: tc.err}, Audit: playerAudit{}}
			})
			b.login(t)
			response := b.request("PATCH", "/api/players/accounts/2000000", `{"state":0}`, "", true)
			expectStatus(t, response, 409)
			if !strings.Contains(response.Body.String(), `"code":"`+tc.code+`"`) || !strings.Contains(response.Body.String(), tc.err.Error()) {
				t.Fatal(response.Body.String())
			}
		})
	}
}
