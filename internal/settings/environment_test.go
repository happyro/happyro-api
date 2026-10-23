package settings

import (
	"context"
	"errors"
	gameapi "github.com/happyro/happyro-api/internal/game"
	"strings"
	"testing"
)

type environmentGateway struct {
	err   error
	state gameapi.SettingsState
}

func (g environmentGateway) SettingsState(context.Context) (gameapi.SettingsState, error) {
	return g.state, g.err
}
func (g environmentGateway) BattleConfig(context.Context) (map[string]int64, error) {
	panic("must not read values after rejection")
}
func (g environmentGateway) FenceSettings(context.Context, string, uint64) error {
	panic("must not fence after rejection")
}
func TestEnvironmentOverrideRejectsBeforePublishing(t *testing.T) {
	id := int64(1)
	// Nil file/store/command dependencies prove no write or revision is attempted.
	service := Service{Gateway: environmentGateway{state: gameapi.SettingsState{EnvironmentOverrides: map[string]string{"navigation_teleport_policy": "HAPPYRO_NAVIGATION_TELEPORT_POLICY"}}}}
	_, err := service.Apply(context.Background(), Update{Key: "test", Changes: map[string]int64{"base_exp_rate": 200, "navigation_teleport_policy": 2}}, gameapi.Actor{AdminUserID: &id})
	var validation *gameapi.ValidationError
	if !errors.As(err, &validation) || !strings.Contains(validation.Message, "HAPPYRO_NAVIGATION_TELEPORT_POLICY") {
		t.Fatalf("expected named environment lock: %v", err)
	}
}
func TestEnvironmentReadFailureDoesNotPublish(t *testing.T) {
	id := int64(1)
	unavailable := errors.New("map unavailable")
	service := Service{Gateway: environmentGateway{err: unavailable}}
	_, err := service.Apply(context.Background(), Update{Key: "test", Changes: map[string]int64{"base_exp_rate": 200}}, gameapi.Actor{AdminUserID: &id})
	if !errors.Is(err, unavailable) {
		t.Fatalf("expected map error: %v", err)
	}
}
