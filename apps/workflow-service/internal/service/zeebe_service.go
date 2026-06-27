package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
	"github.com/camunda/zeebe/clients/go/v8/pkg/zbc"
)

type ZeebeService struct {
	client zbc.Client
}

func NewZeebeService(addr string) (*ZeebeService, error) {
	client, err := zbc.NewClient(&zbc.ClientConfig{
		GatewayAddress:         addr,
		UsePlaintextConnection: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create zeebe client: %w", err)
	}

	slog.Info("Connected to Zeebe gateway", "addr", addr)
	return &ZeebeService{client: client}, nil
}

func (s *ZeebeService) Close() {
	if s.client != nil {
		_ = s.client.Close()
	}
}

func (s *ZeebeService) DeployWorkflow(ctx context.Context, name string, content []byte) (int64, error) {
	resp, err := s.client.NewDeployResourceCommand().
		AddResource(content, name).
		Send(ctx)
	if err != nil {
		return 0, fmt.Errorf("deploy bpmn: %w", err)
	}

	if len(resp.GetDeployments()) > 0 {
		dep := resp.GetDeployments()[0]
		if process := dep.GetProcess(); process != nil {
			return process.GetProcessDefinitionKey(), nil
		}
	}

	return 0, nil
}

func (s *ZeebeService) StartWorkflow(ctx context.Context, bpmnProcessID string, variables map[string]any) (int64, error) {
	cmd := s.client.NewCreateInstanceCommand().
		BPMNProcessId(bpmnProcessID).
		LatestVersion()

	if len(variables) > 0 {
		cmdWithVars, err := cmd.VariablesFromMap(variables)
		if err != nil {
			return 0, fmt.Errorf("set variables: %w", err)
		}
		cmd = cmdWithVars
	}

	resp, err := cmd.Send(ctx)
	if err != nil {
		return 0, fmt.Errorf("start workflow: %w", err)
	}

	return resp.GetProcessInstanceKey(), nil
}

func (s *ZeebeService) PublishMessage(ctx context.Context, messageName string, correlationKey string, messageID string, variables map[string]any) (int64, error) {
	cmd := s.client.NewPublishMessageCommand().
		MessageName(messageName).
		CorrelationKey(correlationKey)

	if messageID != "" {
		cmd = cmd.MessageId(messageID)
	}

	if len(variables) > 0 {
		cmdWithVars, err := cmd.VariablesFromMap(variables)
		if err != nil {
			return 0, fmt.Errorf("set message variables: %w", err)
		}
		cmd = cmdWithVars
	}

	resp, err := cmd.Send(ctx)
	if err != nil {
		return 0, fmt.Errorf("publish message: %w", err)
	}

	_ = resp // publish message doesn't return key directly
	return 0, nil
}

func (s *ZeebeService) CancelWorkflow(ctx context.Context, processInstanceKey int64) error {
	_, err := s.client.NewCancelInstanceCommand().
		ProcessInstanceKey(processInstanceKey).
		Send(ctx)
	if err != nil {
		return fmt.Errorf("cancel workflow instance %d: %w", processInstanceKey, err)
	}
	return nil
}

func (s *ZeebeService) NewJobWorker(jobType string, handler func(client worker.JobClient, job entities.Job)) worker.JobWorker {
	return s.client.NewJobWorker().
		JobType(jobType).
		Handler(handler).
		Open()
}

