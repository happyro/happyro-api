package game

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

func TestGatewayPreservesObjectPayloadAndAuthentication(t *testing.T) {
	gateway := &HTTPGateway{BaseURL: "http://game.local", Token: "secret", Client: doerFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/game-control/v1/commands" || request.Method != "POST" {
			t.Fatalf("wrong endpoint: %s %s", request.Method, request.URL)
		}
		if request.Header.Get("Authorization") != "Bearer secret" || request.Header.Get("Idempotency-Key") != "operation-1" {
			t.Fatal("missing authentication or idempotency key")
		}
		if request.GetBody != nil {
			t.Fatal("mutating command allows implicit transport replay")
		}
		var envelope map[string]json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		if string(envelope["payload"]) != "{}" {
			t.Fatalf("empty payload must be object, got %s", envelope["payload"])
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"result":{"large":9007199254740993}}}`))}, nil
	})}
	result, err := gateway.Execute(context.Background(), Command{ID: "id", Type: "character.stats.reset", IdempotencyKey: "operation-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result["large"].(json.Number).String() != "9007199254740993" {
		t.Fatal("integer precision lost")
	}
}

func TestGatewayClassifiesUncertainWriteOutcomes(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		status         int
		body           string
		transportError bool
		unknown        bool
	}{
		{"connection failure", 0, "", true, true},
		{"server failure", 500, `{"error":{"code":"internal_error"}}`, false, true},
		{"invalid success", 200, `{"data":{"result":null}}`, false, true},
		{"rejected", 422, `{"error":{"code":"character_offline","message":"offline"}}`, false, false},
		{"unauthorized", 401, `{}`, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			gateway := &HTTPGateway{BaseURL: "http://game.local", Token: "secret", Client: doerFunc(func(*http.Request) (*http.Response, error) {
				if scenario.transportError {
					return nil, errors.New("connection reset")
				}
				return &http.Response{StatusCode: scenario.status, Body: io.NopCloser(strings.NewReader(scenario.body))}, nil
			})}
			_, err := gateway.Execute(context.Background(), Command{Type: "monster.spawn"})
			var failure *GatewayError
			if !errors.As(err, &failure) || failure.OutcomeUnknown != scenario.unknown {
				t.Fatalf("unexpected classification: %#v", err)
			}
		})
	}
}

func TestSnapshotRejectsAnotherCharactersData(t *testing.T) {
	gateway := &HTTPGateway{BaseURL: "http://game.local", Token: "secret", Client: doerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":{"result":{"char_id":2}}}`))}, nil
	})}
	_, err := gateway.CharacterSnapshot(context.Background(), 1)
	var failure *GatewayError
	if !errors.As(err, &failure) || failure.Code != "invalid_response" || failure.OutcomeUnknown {
		t.Fatalf("wrong snapshot accepted: %v", err)
	}
}
