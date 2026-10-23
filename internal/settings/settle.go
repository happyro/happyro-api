package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// RecoverPending is safe to run in every API process: publication and recovery
// share the database lock, and contention simply waits for the next sweep.
func (s *Service) RecoverPending(ctx context.Context) (err error) {
	session, err := s.Revisions.Acquire(ctx)
	if errors.Is(err, ErrBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, session.Close()) }()
	rows, err := session.Pending(ctx)
	if err != nil {
		return err
	}
	for i := range rows {
		if err = s.settle(ctx, session, &rows[i]); err != nil {
			return err
		}
	}
	return nil
}

// settle releases an interrupted publication using live map evidence. It never
// resends the old apply command. A barrier invalidates even a delayed command
// whose receipt was lost, before the next publication can be admitted.
func (s *Service) settle(ctx context.Context, session RevisionSession, row *Revision) error {
	state, err := s.Gateway.SettingsState(ctx)
	if err != nil {
		return err
	}
	if (row.MapInstance == nil) != (row.MapRevision == nil) {
		return ErrUnconfirmed
	}
	if row.MapInstance != nil && state.InstanceID == *row.MapInstance {
		if state.Revision < *row.MapRevision {
			return ErrUnconfirmed
		}
		if state.Revision == *row.MapRevision {
			fenceErr := s.Gateway.FenceSettings(ctx, state.InstanceID, state.Revision)
			state, err = s.Gateway.SettingsState(ctx)
			if err != nil {
				return err
			}
			if state.InstanceID == *row.MapInstance && state.Revision <= *row.MapRevision {
				if fenceErr != nil {
					return fenceErr
				}
				return ErrUnconfirmed
			}
		}
	}
	if state.LastStatus == "running" {
		return ErrUnconfirmed
	}
	var changes map[string]int64
	if err = json.Unmarshal(row.Changes, &changes); err != nil {
		return err
	}
	actual := make(map[string]int64, len(changes))
	applied := true
	for key, value := range changes {
		live, ok := state.Values[key]
		if !ok {
			return fmt.Errorf("无法读取游戏设置 %s 的实际值", key)
		}
		actual[key] = live
		applied = applied && live == value
	}
	// Keep the existing effective settings when the abandoned change did not
	// apply. Explicit values also survive a restart with different defaults.
	restored, err := Render(row.Previous, actual)
	if err != nil {
		return err
	}
	current, err := s.File.Read()
	if err != nil {
		return err
	}
	// Accept our own interrupted recovery write, but never overwrite an operator edit.
	if current != row.Previous && current != row.Desired && current != restored {
		return ErrFileChanged
	}
	target := restored
	if applied {
		target = row.Desired
	}
	if err = s.File.Publish(target); err != nil {
		return err
	}
	if !applied {
		code := "settings_auto_restored"
		return session.Transition(ctx, row.ID, row.Status, "failed", &code)
	}
	if row.Status == "prepared" {
		if err = session.Transition(ctx, row.ID, "prepared", "published", nil); err != nil {
			return err
		}
		row.Status = "published"
	}
	return session.Transition(ctx, row.ID, row.Status, "applied", nil)
}
