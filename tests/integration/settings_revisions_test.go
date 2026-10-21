package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	gameapi "github.com/happyro/happyro-api/internal/game"
	"github.com/happyro/happyro-api/internal/settings"
)

func TestSettingsRevisionsSerializeAndRetainUnfinishedPublication(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	repo := &settings.SQLRevisions{DB: db}
	first, err := repo.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := repo.Lock(ctx); !errors.Is(err, settings.ErrBusy) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("parallel lock: %v", err)
	}
	actorID := int64(1)
	row, err := first.Create(ctx, "request", strings.Repeat("a", 64), map[string]int64{"base_exp_rate": 200}, "before", "after", gameapi.Actor{AdminUserID: &actorID})
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Close(); err != nil {
		t.Fatal(err)
	}
	// A new process/session cannot replace a pending revision merely because its
	// predecessor released the connection or crashed between publication steps.
	second, err := repo.Lock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err = second.Create(ctx, "other", strings.Repeat("b", 64), map[string]int64{"base_exp_rate": 300}, "before", "after", gameapi.Actor{AdminUserID: &actorID}); !errors.Is(err, settings.ErrBusy) {
		t.Fatalf("unfinished revision bypassed: %v", err)
	}
	stored, err := second.Find(ctx, "request")
	if err != nil || stored == nil || stored.Previous != "before" || stored.Desired != "after" || stored.Status != "prepared" {
		t.Fatalf("intent lost: %+v %v", stored, err)
	}
	if err = second.Transition(ctx, row.ID, "prepared", "applied", nil); err == nil {
		t.Fatal("skipped publication state")
	}
	if err = second.Transition(ctx, row.ID, "prepared", "published", nil); err != nil {
		t.Fatal(err)
	}
	code := "outcome_unknown"
	if err = second.Transition(ctx, row.ID, "published", "uncertain", &code); err != nil {
		t.Fatal(err)
	}
	if _, err = second.Create(ctx, "other", strings.Repeat("b", 64), map[string]int64{"base_exp_rate": 300}, "before", "after", gameapi.Actor{AdminUserID: &actorID}); !errors.Is(err, settings.ErrBusy) {
		t.Fatalf("uncertain revision bypassed: %v", err)
	}
	if err = second.Transition(ctx, row.ID, "uncertain", "applied", nil); err != nil {
		t.Fatal(err)
	}
	if err = second.Transition(ctx, row.ID, "uncertain", "failed", nil); err == nil {
		t.Fatal("stale state accepted")
	}
	if _, err = second.Create(ctx, "other", strings.Repeat("b", 64), map[string]int64{"base_exp_rate": 300}, "after", "next", gameapi.Actor{AdminUserID: &actorID}); err != nil {
		t.Fatal(err)
	}

	history, err := repo.History(ctx, 1, 1)
	if err != nil || len(history.Data) != 1 || history.Meta["total"] != 2 || history.Meta["last_page"] != 2 {
		t.Fatalf("history: %+v %v", history, err)
	}
	if history.Definitions["refine_chance_rate"].Unit != "percent" {
		t.Fatal("history omitted setting units")
	}
	if history.Data[0].Previous != "" || history.Data[0].Desired != "" || history.Data[0].Hash != "" {
		t.Fatal("history loaded private publication fields")
	}
}
