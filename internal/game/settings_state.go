package game

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// SettingsState is an atomic observation from the map event loop. Values alone
// are not evidence that an earlier settings command can no longer execute.
type SettingsState struct {
	Values               map[string]int64  `json:"values"`
	EnvironmentOverrides map[string]string `json:"environment_overrides"`
	InstanceID           string            `json:"instance_id"`
	Revision             uint64            `json:"revision"`
	LastCommandID        string            `json:"last_command_id"`
	LastStatus           string            `json:"last_status"`
}

func (g *HTTPGateway) SettingsState(ctx context.Context) (SettingsState, error) {
	raw, err := g.call(ctx, "GET", "/battle-config", "", nil, false)
	if err != nil {
		return SettingsState{}, err
	}
	// A missing revision must not silently become zero and admit a new command.
	var envelope struct {
		Result struct {
			Values               map[string]int64  `json:"values"`
			EnvironmentOverrides map[string]string `json:"environment_overrides"`
			InstanceID           string            `json:"instance_id"`
			Revision             *uint64           `json:"revision"`
			LastCommandID        *string           `json:"last_command_id"`
			LastStatus           string            `json:"last_status"`
		} `json:"result"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return SettingsState{}, invalidResponse(false)
	}
	result := envelope.Result
	instance, decodeErr := hex.DecodeString(result.InstanceID)
	if decodeErr != nil || len(instance) != 16 || result.Values == nil || result.Revision == nil || result.LastCommandID == nil {
		return SettingsState{}, invalidResponse(false)
	}
	commandID := *result.LastCommandID
	switch result.LastStatus {
	case "idle":
		if *result.Revision != 0 || commandID != "" {
			return SettingsState{}, invalidResponse(false)
		}
	case "fenced":
		if *result.Revision == 0 || commandID != "" {
			return SettingsState{}, invalidResponse(false)
		}
	case "running", "succeeded", "failed":
		if *result.Revision == 0 || commandID == "" || len(commandID) > 64 || strings.ContainsRune(commandID, 0) {
			return SettingsState{}, invalidResponse(false)
		}
	default:
		return SettingsState{}, invalidResponse(false)
	}
	return SettingsState{Values: result.Values, EnvironmentOverrides: result.EnvironmentOverrides, InstanceID: result.InstanceID, Revision: *result.Revision, LastCommandID: commandID, LastStatus: result.LastStatus}, nil
}

// FenceSettings advances the map version without changing any game setting.
// If its response is lost, callers must query state; never assume it won the race.
func (g *HTTPGateway) FenceSettings(ctx context.Context, instance string, revision uint64) error {
	id, err := NewID()
	if err != nil {
		return err
	}
	_, err = g.Execute(ctx, Command{ID: id, Type: "battle_config.fence", Payload: map[string]any{"instance_id": instance, "expected_revision": revision}})
	return err
}
