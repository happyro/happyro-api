package game

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/happyro/happyro-api/internal/catalog"
)

type ValidationError struct{ Field, Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(field string) *ValidationError {
	return &ValidationError{Field: field, Message: "游戏命令参数无效：" + field + "。"}
}

func Integer(value any) (int64, bool) {
	switch value := value.(type) {
	case json.Number:
		n, err := value.Int64()
		return n, err == nil
	case int:
		return int64(value), true
	case int64:
		return value, true
	default:
		return 0, false
	}
}
func only(payload map[string]any, keys ...string) error {
	for key := range payload {
		if !slices.Contains(keys, key) {
			return invalid("payload." + key)
		}
	}
	return nil
}
func numeric(payload map[string]any, key string, min, max int64, required bool) error {
	value, present := payload[key]
	if !present && !required {
		return nil
	}
	n, ok := Integer(value)
	if !ok || n < min || n > max {
		return invalid("payload." + key)
	}
	return nil
}
func fields(payload map[string]any, limits map[string][2]int64) error {
	if len(payload) == 0 {
		return invalid("payload")
	}
	for key, value := range payload {
		bounds, ok := limits[key]
		if !ok {
			return invalid("payload." + key)
		}
		n, ok := Integer(value)
		if !ok || n < bounds[0] || n > bounds[1] {
			return invalid("payload." + key)
		}
	}
	return nil
}

var navigationMap = regexp.MustCompile(`^[a-zA-Z0-9_@-]{1,15}$`)

func ValidateOperator(request Request) error {
	if len(request.IdempotencyKey) < 1 || len(request.IdempotencyKey) > 64 || strings.TrimSpace(request.IdempotencyKey) != request.IdempotencyKey {
		return invalid("idempotency_key")
	}
	if request.Target == nil || request.Target.Type != "character" {
		return invalid("target.type")
	}
	id, err := strconv.ParseInt(request.Target.ID, 10, 32)
	if err != nil || id <= 0 {
		return invalid("target.id")
	}
	if request.Payload == nil {
		return invalid("payload")
	}
	p := request.Payload
	switch request.Type {
	case "character.progression.update":
		return fields(p, map[string][2]int64{"base_level": {1, math.MaxInt32}, "job_level": {1, math.MaxInt32}, "job_id": {0, math.MaxInt32}})
	case "character.points.update":
		return fields(p, map[string][2]int64{"skill_points": {0, 32767}, "status_points": {0, math.MaxInt32}, "trait_points": {0, math.MaxInt32}})
	case "character.skill_points.update":
		return fields(p, map[string][2]int64{"skill_points": {0, 32767}})
	case "character.stats.update":
		return fields(p, map[string][2]int64{"str": {1, math.MaxInt32}, "agi": {1, math.MaxInt32}, "vit": {1, math.MaxInt32}, "int": {1, math.MaxInt32}, "dex": {1, math.MaxInt32}, "luk": {1, math.MaxInt32}})
	case "character.traits.update":
		return fields(p, map[string][2]int64{"pow": {0, 32767}, "sta": {0, 32767}, "wis": {0, 32767}, "spl": {0, 32767}, "con": {0, 32767}, "crt": {0, 32767}})
	case "character.stats.reset", "character.traits.reset", "character.skills.reset", "character.skills.learn_all":
		return only(p)
	case "character.appearance.update":
		if err := only(p, "appearance"); err != nil {
			return err
		}
		n, ok := Integer(p["appearance"])
		if !ok || !catalog.ValidAppearance(n) {
			return invalid("payload.appearance")
		}
		return nil
	case "character.riding.update":
		return validateRiding(p)
	case "character.vitals.restore":
		return validateVitals(p)
	case "character.navigation.route", "character.navigation.teleport":
		return validateNavigation(request.Type, p)
	case "monster.spawn":
		if err := only(p, "monster_id", "count", "radius", "duration_seconds"); err != nil {
			return err
		}
		for key, bounds := range map[string][2]int64{"monster_id": {1, math.MaxInt32}, "count": {1, 10}, "radius": {1, 10}, "duration_seconds": {1, 3600}} {
			if err := numeric(p, key, bounds[0], bounds[1], key == "monster_id"); err != nil {
				return err
			}
		}
		return nil
	default:
		return invalid("type")
	}
}
func validateRiding(p map[string]any) error {
	if err := only(p, "kind", "enabled", "fee", "ticket"); err != nil {
		return err
	}
	kind, ok := p["kind"].(string)
	if !ok || !slices.Contains([]string{"cart", "peco", "dragon", "gryphon", "mado", "falcon", "warg", "warg_rider", "hawk"}, kind) {
		return invalid("payload.kind")
	}
	for _, key := range []string{"enabled", "ticket"} {
		if _, ok := p[key].(bool); !ok {
			return invalid("payload." + key)
		}
	}
	return numeric(p, "fee", 0, 3500, true)
}
func validateVitals(p map[string]any) error {
	if err := only(p, "vitals"); err != nil {
		return err
	}
	values, present := p["vitals"]
	if !present {
		return nil
	}
	list, ok := values.([]any)
	if !ok {
		return invalid("payload.vitals")
	}
	seen := map[string]bool{}
	for _, value := range list {
		name, ok := value.(string)
		if !ok || !slices.Contains([]string{"hp", "sp", "ap"}, name) || seen[name] {
			return invalid("payload.vitals")
		}
		seen[name] = true
	}
	return nil
}
func validateNavigation(kind string, p map[string]any) error {
	keys := []string{"map", "x", "y", "npc_class"}
	if kind == "character.navigation.route" {
		action, ok := p["action"].(string)
		if !ok {
			return invalid("payload.action")
		}
		if action == "stop" {
			return only(p, "action")
		}
		if action != "walk" && action != "preview" {
			return invalid("payload.action")
		}
		keys = append(keys, "action")
	}
	if err := only(p, keys...); err != nil {
		return err
	}
	name, ok := p["map"].(string)
	if !ok || !navigationMap.MatchString(name) {
		return invalid("payload.map")
	}
	for _, axis := range []string{"x", "y"} {
		if err := numeric(p, axis, 0, 32767, true); err != nil {
			return err
		}
	}
	return numeric(p, "npc_class", 1, 32767, false)
}

func (s *Service) SubmitOperator(ctx context.Context, request Request, userID int64) (Command, bool, error) {
	if userID <= 0 {
		return Command{}, false, fmt.Errorf("invalid admin actor")
	}
	if err := ValidateOperator(request); err != nil {
		return Command{}, false, err
	}
	return s.Submit(ctx, request, Actor{AdminUserID: &userID})
}
