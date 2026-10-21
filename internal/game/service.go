package game

import (
	"context"
	"errors"
	"strings"
	"time"
)

type Service struct {
	Commands Repository
	Gateway  Gateway
	Timeout  time.Duration
}

func (s *Service) Submit(ctx context.Context, request Request, actor Actor) (Command, bool, error) {
	if !actor.Valid() {
		return Command{}, false, errors.New("invalid operation actor")
	}
	if len(request.IdempotencyKey) == 0 || len(request.IdempotencyKey) > 64 || strings.TrimSpace(request.IdempotencyKey) != request.IdempotencyKey {
		return Command{}, false, errors.New("幂等键必须包含 1 至 64 个字节。")
	}
	if request.Payload == nil {
		request.Payload = map[string]any{}
	}
	command, created, err := s.Commands.Submit(ctx, request, actor)
	if err != nil {
		return Command{}, false, err
	}
	if command.Status == "succeeded" {
		return command, created, nil
	}
	if command.Status == "failed" || command.Status == "indeterminate" {
		code, message := "command_not_replayable", "此命令不能重复执行，请核对原始操作结果。"
		if command.ErrorCode != nil {
			code = *command.ErrorCode
		}
		if command.ErrorMessage != nil {
			message = *command.ErrorMessage
		}
		return command, created, &GatewayError{code, message, command.Status == "indeterminate"}
	}
	if command.Status != "pending" {
		return command, created, ErrRunning
	}
	command, err = s.Commands.Claim(ctx, command.ID, s.Timeout+15*time.Second)
	if err != nil {
		return command, created, err
	}
	execute, cancel := context.WithTimeout(ctx, s.Timeout)
	defer cancel()
	result, executeErr := s.Gateway.Execute(execute, command)
	status := "succeeded"
	var failure *GatewayError
	if executeErr != nil {
		if !errors.As(executeErr, &failure) {
			failure = &GatewayError{Code: "unexpected_error", Message: "无法确认游戏命令的执行结果。", OutcomeUnknown: true}
		}
		status = "failed"
		if failure.OutcomeUnknown {
			status = "indeterminate"
		}
	}
	// Persist the result even when the caller disconnected or timed out. Never
	// send the external command a second time to repair a persistence failure.
	persist, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelPersist()
	updated, err := s.Commands.Finish(persist, command.ID, status, result, failure)
	if err != nil {
		return command, created, err
	}
	if executeErr != nil {
		return updated, created, failure
	}
	return updated, created, nil
}

// ResolveSettings records evidence obtained by the serialized settings recovery
// use case. It cannot resolve other command kinds or interrupt a live lease.
func (s *Service) ResolveSettings(ctx context.Context, id string, succeeded bool) (Command, error) {
	command, err := s.Commands.Find(ctx, id)
	if err != nil {
		return command, err
	}
	if command.Type != "battle_config.apply" || command.Status != "indeterminate" {
		return command, ErrRunning
	}
	if succeeded {
		return s.Commands.Finish(ctx, id, "succeeded", map[string]any{"reconciled": true}, nil)
	}
	failure := &GatewayError{Code: "settings_fenced", Message: "原设置命令已终止，可以重新应用。"}
	command, err = s.Commands.Finish(ctx, id, "failed", nil, failure)
	if err != nil {
		return command, err
	}
	return command, failure
}
