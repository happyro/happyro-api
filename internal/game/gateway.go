package game

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}
type HTTPGateway struct {
	Client         HTTPDoer
	BaseURL, Token string
}

var errorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func (g *HTTPGateway) call(ctx context.Context, method, path, key string, body any, sideEffects bool) (json.RawMessage, error) {
	if g.Token == "" {
		return nil, &GatewayError{Code: "authentication_failed", Message: "未配置游戏控制令牌。"}
	}
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(g.BaseURL, "/")+"/game-control/v1"+path, input)
	if err != nil {
		return nil, err
	}
	// The Go transport treats Idempotency-Key as permission to replay a body.
	// Runtime game commands must never be retried implicitly after an I/O error.
	if sideEffects {
		req.GetBody = nil
	}
	req.Header.Set("Authorization", "Bearer "+g.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := g.Client.Do(req)
	if err != nil {
		return nil, &GatewayError{Code: "unavailable", Message: "游戏服务暂时不可用。", OutcomeUnknown: sideEffects}
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return nil, invalidResponse(sideEffects)
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &envelope)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure := &GatewayError{Code: "request_failed", Message: "游戏服务请求失败。", OutcomeUnknown: sideEffects}
		switch response.StatusCode {
		case 401, 403:
			failure.Code = "authentication_failed"
			failure.Message = "游戏服务认证失败。"
			failure.OutcomeUnknown = false
		case 404, 501:
			failure.Code = "unsupported_command"
			failure.Message = "游戏服务不支持此命令。"
			failure.OutcomeUnknown = false
		case 400, 409, 422:
			failure.Code = "request_rejected"
			failure.Message = "游戏服务拒绝了此请求。"
			failure.OutcomeUnknown = false
			if decodeErr == nil && errorCodePattern.MatchString(envelope.Error.Code) && len([]rune(envelope.Error.Message)) <= 255 {
				failure.Code = envelope.Error.Code
				if envelope.Error.Message != "" {
					failure.Message = envelope.Error.Message
				}
			}
		}
		return nil, failure
	}
	if decodeErr != nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, invalidResponse(sideEffects)
	}
	return envelope.Data, nil
}
func invalidResponse(unknown bool) *GatewayError {
	return &GatewayError{Code: "invalid_response", Message: "游戏服务返回了无效响应。", OutcomeUnknown: unknown}
}

func (g *HTTPGateway) Capabilities(ctx context.Context) (Capabilities, error) {
	raw, err := g.call(ctx, "GET", "/capabilities", "", nil, false)
	if err != nil {
		return Capabilities{}, err
	}
	var result Capabilities
	if err = json.Unmarshal(raw, &result); err != nil || result.ProtocolVersion == "" || result.Commands == nil {
		return result, invalidResponse(false)
	}
	return result, nil
}
func (g *HTTPGateway) Execute(ctx context.Context, command Command) (map[string]any, error) {
	payload := command.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	raw, err := g.call(ctx, "POST", "/commands", command.IdempotencyKey, struct {
		ID      string         `json:"id"`
		Type    string         `json:"type"`
		Target  *Target        `json:"target"`
		Payload map[string]any `json:"payload"`
	}{command.ID, command.Type, command.Target(), payload}, true)
	if err != nil {
		return nil, err
	}
	var result struct {
		Result map[string]any `json:"result"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&result); err != nil || result.Result == nil {
		return nil, invalidResponse(true)
	}
	return result.Result, nil
}
func (g *HTTPGateway) BattleConfig(ctx context.Context) (map[string]int64, error) {
	raw, err := g.call(ctx, "GET", "/battle-config", "", nil, false)
	if err != nil {
		return nil, err
	}
	var result struct {
		Result struct {
			Values map[string]int64 `json:"values"`
		} `json:"result"`
	}
	if err = json.Unmarshal(raw, &result); err != nil || result.Result.Values == nil {
		return nil, invalidResponse(false)
	}
	return result.Result.Values, nil
}
func (g *HTTPGateway) CharacterSnapshot(ctx context.Context, id int64) (map[string]any, error) {
	commandID, err := NewID()
	if err != nil {
		return nil, err
	}
	targetType, targetID := "character", strconv.FormatInt(id, 10)
	result, err := g.Execute(ctx, Command{ID: commandID, Type: "character.snapshot", TargetType: &targetType, TargetID: &targetID, Payload: map[string]any{}})
	if err != nil {
		var gatewayErr *GatewayError
		if errors.As(err, &gatewayErr) {
			gatewayErr.OutcomeUnknown = false
		}
		return nil, err
	}
	number, ok := result["char_id"].(json.Number)
	if !ok {
		return nil, invalidResponse(false)
	}
	actual, err := number.Int64()
	if err != nil || actual != id {
		return nil, invalidResponse(false)
	}
	return result, nil
}
