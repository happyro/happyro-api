package integration

import (
	"context"
	sessionauth "github.com/happyro/happyro-api/internal/session"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPlayerLimitsAreAtomicAndSeparateAccountsAndCategories(t *testing.T) {
	db := testDatabase(t)
	limiter := &sessionauth.SQLPlayerLimiter{DB: db}
	ctx := context.Background()
	var allowed atomic.Int32
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			retry, err := limiter.Allow(ctx, 2000000, "action", 5)
			if err != nil {
				t.Error(err)
				return
			}
			if retry == 0 {
				allowed.Add(1)
			} else if retry < 1 || retry > 60 {
				t.Errorf("retry=%d", retry)
			}
		})
	}
	workers.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed %d, want 5", allowed.Load())
	}
	for _, entry := range []struct {
		account  int64
		category string
	}{{2000001, "action"}, {2000000, "read"}, {2000000, "assets"}} {
		if retry, err := limiter.Allow(ctx, entry.account, entry.category, 5); err != nil || retry != 0 {
			t.Fatalf("independent bucket: %d %v", retry, err)
		}
	}
	if _, err := db.Exec("UPDATE adventure_limits SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND WHERE account_id=2000000 AND category='action'"); err != nil {
		t.Fatal(err)
	}
	if retry, err := limiter.Allow(ctx, 2000000, "action", 5); err != nil || retry != 0 {
		t.Fatalf("expired bucket: %d %v", retry, err)
	}
}
