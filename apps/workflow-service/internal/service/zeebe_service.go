package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/camunda/zeebe/clients/go/v8/pkg/entities"
	"github.com/camunda/zeebe/clients/go/v8/pkg/worker"
	"github.com/camunda/zeebe/clients/go/v8/pkg/zbc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ZeebeService struct {
	client zbc.Client
}

type WorkflowTask struct {
	JobKey             int64          `json:"jobKey"`
	Type               string         `json:"type"`
	ElementID          string         `json:"elementId"`
	ProcessInstanceKey int64          `json:"processInstanceKey"`
	CaseID             string         `json:"caseId"`
	CaseCode           string         `json:"caseCode"`
	CustomerID         string         `json:"customerId"`
	CustomerName       string         `json:"customerName"`
	CandidateRole      string         `json:"candidateRole"`
	FormKey            string         `json:"formKey"`
	SLADueAt           *time.Time     `json:"slaDueAt,omitempty"`
	Variables          map[string]any `json:"variables"`
}

func (t WorkflowTask) MarshalJSON() ([]byte, error) {
	payload := map[string]any{
		"jobKey":             strconv.FormatInt(t.JobKey, 10),
		"type":               t.Type,
		"elementId":          t.ElementID,
		"processInstanceKey": strconv.FormatInt(t.ProcessInstanceKey, 10),
		"caseId":             t.CaseID,
		"caseCode":           t.CaseCode,
		"customerId":         t.CustomerID,
		"customerName":       t.CustomerName,
		"candidateRole":      t.CandidateRole,
		"formKey":            t.FormKey,
		"variables":          t.Variables,
	}
	if t.SLADueAt != nil {
		payload["slaDueAt"] = t.SLADueAt
	}
	return json.Marshal(payload)
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

func (s *ZeebeService) RetryJob(ctx context.Context, jobKey int64, retries int32) error {
	if jobKey <= 0 {
		return fmt.Errorf("invalid job key")
	}
	if retries <= 0 {
		retries = 3
	}
	_, err := s.client.NewUpdateJobRetriesCommand().JobKey(jobKey).Retries(retries).Send(ctx)
	if err != nil {
		return fmt.Errorf("update job retries: %w", err)
	}
	slog.Info("zeebe job retries updated", "jobKey", jobKey, "retries", retries)
	return nil
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

func (s *ZeebeService) ActivateTasks(ctx context.Context, jobType, workerName string, maxJobs int32) ([]WorkflowTask, error) {
	if maxJobs <= 0 || maxJobs > 20 {
		maxJobs = 10
	}
	jobs, err := s.client.NewActivateJobsCommand().
		JobType(jobType).
		MaxJobsToActivate(maxJobs).
		WorkerName(workerName).
		Timeout(30 * time.Minute).
		FetchVariables(taskFetchVariables...).
		Send(ctx)
	if err != nil {
		return nil, fmt.Errorf("activate jobs: %w", err)
	}

	tasks := make([]WorkflowTask, 0, len(jobs))
	for _, job := range jobs {
		tasks = append(tasks, workflowTaskFromJob(job))
	}
	return tasks, nil
}

func (s *ZeebeService) ClaimNextTask(ctx context.Context, jobType, workerName string) (*WorkflowTask, error) {
	return s.ClaimTaskForProcess(ctx, jobType, workerName, TaskClaimFilter{})
}

func (s *ZeebeService) ClaimTaskForProcess(ctx context.Context, jobType, workerName string, filter TaskClaimFilter) (*WorkflowTask, error) {
	return s.claimTaskForProcess(ctx, jobType, workerName, filter, 30*time.Minute)
}

func (s *ZeebeService) ClaimInboxTask(ctx context.Context, jobType, workerName string, filter TaskClaimFilter) (*WorkflowTask, error) {
	return s.claimTaskForProcess(ctx, jobType, workerName, filter, 2*time.Minute)
}

func (s *ZeebeService) ResolveInboxTask(ctx context.Context, role string, filter TaskClaimFilter) (*WorkflowTask, error) {
	jobTypes := inboxJobTypesForRole(role)
	if filter.ProcessInstanceKey > 0 || filter.CaseID != "" {
		if primary := taskTypeForRole(role); primary != "" {
			jobTypes = []string{primary}
		}
	}
	slog.Info("zeebe resolve inbox task",
		"role", role,
		"jobTypes", jobTypes,
		"caseId", filter.CaseID,
		"elementId", filter.ElementID,
		"processInstanceKey", filter.ProcessInstanceKey,
	)
	if err := s.HealthCheck(ctx); err != nil {
		slog.Error("zeebe health check failed before inbox resolve",
			"processInstanceKey", filter.ProcessInstanceKey,
			"err", err,
		)
		return nil, fmt.Errorf("zeebe unreachable: %w", err)
	}
	var lastErr error
	for _, dropElement := range []bool{false, true} {
		current := filter
		if dropElement {
			current.ElementID = ""
		}
		for _, jobType := range jobTypes {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("resolve inbox task timed out: %w", err)
			}
			task, err := s.ClaimInboxTask(ctx, jobType, "arda-workflow-inbox", current)
			if err == nil {
				slog.Info("zeebe resolve inbox task matched",
					"role", role,
					"jobType", jobType,
					"dropElement", dropElement,
					"jobKey", task.JobKey,
					"processInstanceKey", task.ProcessInstanceKey,
					"elementId", task.ElementID,
					"caseId", task.CaseID,
				)
				return task, nil
			}
			lastErr = err
		}
	}
	slog.Warn("zeebe resolve inbox task found no jobs",
		"role", role,
		"processInstanceKey", filter.ProcessInstanceKey,
		"caseId", filter.CaseID,
		"lastErr", lastErr,
	)
	if filter.ProcessInstanceKey > 0 && lastErr != nil {
		return nil, fmt.Errorf("no inbox task available for process instance %d — %w", filter.ProcessInstanceKey, lastErr)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no inbox task available")
}

type ProcessJobSnapshot struct {
	JobKey             int64  `json:"-"`
	JobType            string `json:"jobType"`
	ElementID          string `json:"elementId"`
	ProcessInstanceKey int64  `json:"-"`
	CaseID             string `json:"caseId,omitempty"`
	Retries            int32  `json:"retries"`
	State              string `json:"state"`
	ErrorMessage       string `json:"errorMessage,omitempty"`
}

func (job ProcessJobSnapshot) MarshalJSON() ([]byte, error) {
	payload := map[string]any{
		"jobType":            job.JobType,
		"elementId":          job.ElementID,
		"processInstanceKey": strconv.FormatInt(job.ProcessInstanceKey, 10),
		"jobKey":             strconv.FormatInt(job.JobKey, 10),
		"retries":            job.Retries,
		"state":              job.State,
	}
	if job.CaseID != "" {
		payload["caseId"] = job.CaseID
	}
	if job.ErrorMessage != "" {
		payload["errorMessage"] = job.ErrorMessage
	}
	return json.Marshal(payload)
}

type ProcessIncidentSnapshot struct {
	JobKey       string    `json:"jobKey"`
	JobType      string    `json:"jobType"`
	ElementID    string    `json:"elementId"`
	Retries      int       `json:"retries"`
	ErrorMessage string    `json:"errorMessage"`
	CreatedAt    time.Time `json:"createdAt"`
}

func customerRegistrationJobTypes() []string {
	return []string{
		"crm.mark_customer_submitted",
		"crm.check_customer_duplicate",
		"workflow.customer_checker_review",
		"crm.request_customer_changes",
		"workflow.customer_maker_revise",
		"workflow.customer_risk_review",
		"crm.reject_customer",
		"crm.approve_customer",
		"notification.customer_registration_result",
	}
}

func elementIDToJobType(elementID string) string {
	switch strings.TrimSpace(elementID) {
	case "Activity_MarkSubmitted":
		return "crm.mark_customer_submitted"
	case "Activity_CheckDuplicate":
		return "crm.check_customer_duplicate"
	case "Activity_CheckerReview", "UT_CheckerReview":
		return "workflow.customer_checker_review"
	case "Activity_RequestChanges":
		return "crm.request_customer_changes"
	case "Activity_MakerRevise", "UT_MakerRevise":
		return "workflow.customer_maker_revise"
	case "Activity_RejectCustomer":
		return "crm.reject_customer"
	case "Activity_RiskReview":
		return "workflow.customer_risk_review"
	case "Activity_ApproveCustomer":
		return "crm.approve_customer"
	case "Activity_NotifyResult":
		return "notification.customer_registration_result"
	default:
		return ""
	}
}

func jobTypesForDiagnose(caseType, currentStep string) []string {
	if caseType != "" && caseType != "CUSTOMER_REGISTRATION" {
		return customerRegistrationJobTypes()
	}
	step := strings.TrimSpace(currentStep)
	if step == "" || step == "submitted" {
		return []string{
			"crm.mark_customer_submitted",
			"crm.check_customer_duplicate",
		}
	}
	if jobType := elementIDToJobType(step); jobType != "" {
		return []string{jobType}
	}
	return []string{
		"crm.check_customer_duplicate",
		"crm.request_customer_changes",
		"workflow.customer_checker_review",
	}
}

func (s *ZeebeService) FindProcessJobs(ctx context.Context, processInstanceKey int64) ([]ProcessJobSnapshot, error) {
	return s.findProcessJobsBounded(ctx, processInstanceKey, 1, customerRegistrationJobTypes())
}

func (s *ZeebeService) FindProcessJobsForCase(ctx context.Context, processInstanceKey int64, caseType, currentStep string) ([]ProcessJobSnapshot, error) {
	return s.findProcessJobsBounded(ctx, processInstanceKey, 2, jobTypesForDiagnose(caseType, currentStep))
}

func (s *ZeebeService) findProcessJobsBounded(ctx context.Context, processInstanceKey int64, maxAttemptsPerType int, jobTypes []string) ([]ProcessJobSnapshot, error) {
	if processInstanceKey <= 0 {
		return nil, nil
	}
	if maxAttemptsPerType <= 0 {
		maxAttemptsPerType = 1
	}
	if len(jobTypes) == 0 {
		jobTypes = customerRegistrationJobTypes()
	}
	var found []ProcessJobSnapshot
	const workerName = "arda-workflow-diagnose"
	const jobLockTimeout = 30 * time.Second
	const activateCallBudget = 1200 * time.Millisecond
	for _, jobType := range jobTypes {
		attempts := 0
		for {
			if err := ctx.Err(); err != nil {
				if len(found) > 0 {
					return found, nil
				}
				return found, err
			}
			if attempts >= maxAttemptsPerType {
				break
			}
			callCtx, callCancel := context.WithTimeout(ctx, activateCallBudget)
			jobs, err := s.client.NewActivateJobsCommand().
				JobType(jobType).
				MaxJobsToActivate(1).
				WorkerName(workerName).
				Timeout(jobLockTimeout).
				FetchVariables(taskFetchVariables...).
				Send(callCtx)
			callCancel()
			if err != nil {
				if isTimeoutErr(err) {
					slog.Debug("diagnose activate timed out", "jobType", jobType, "err", err)
					break
				}
				slog.Warn("diagnose activate failed", "jobType", jobType, "err", err)
				break
			}
			if len(jobs) == 0 {
				break
			}
			attempts++
			job := jobs[0]
			task := workflowTaskFromJob(job)
			retries := job.GetRetries()
			state := "pending"
			if retries <= 0 {
				state = "incident"
			}
			if task.ProcessInstanceKey == processInstanceKey {
				found = append(found, ProcessJobSnapshot{
					JobKey:             task.JobKey,
					JobType:            task.Type,
					ElementID:          task.ElementID,
					ProcessInstanceKey: task.ProcessInstanceKey,
					CaseID:             task.CaseID,
					Retries:            retries,
					State:              state,
				})
			}
			if err := s.releaseJob(ctx, job); err != nil {
				return found, err
			}
		}
	}
	if err := ctx.Err(); err != nil {
		if len(found) > 0 {
			return found, nil
		}
		return found, fmt.Errorf("diagnose scan timed out before finding jobs: %w", err)
	}
	return found, nil
}

func isTimeoutErr(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if st, ok := status.FromError(err); ok {
		return st.Code() == codes.DeadlineExceeded || st.Code() == codes.Canceled
	}
	return false
}

func (s *ZeebeService) checkBrokerReachable(ctx context.Context) error {
	_, err := s.client.NewTopologyCommand().Send(ctx)
	return err
}

func (s *ZeebeService) HealthCheck(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.checkBrokerReachable(checkCtx)
}

func inboxJobTypesForRole(role string) []string {
	primary := taskTypeForRole(role)
	seen := map[string]struct{}{}
	out := []string{}
	add := func(jobType string) {
		if jobType == "" {
			return
		}
		if _, ok := seen[jobType]; ok {
			return
		}
		seen[jobType] = struct{}{}
		out = append(out, jobType)
	}
	add(primary)
	for _, jobType := range []string{
		"workflow.customer_maker_revise",
		"workflow.customer_checker_review",
		"workflow.customer_risk_review",
	} {
		add(jobType)
	}
	return out
}

func taskTypeForRole(role string) string {
	switch strings.TrimSpace(role) {
	case "CUSTOMER_CHECKER":
		return "workflow.customer_checker_review"
	case "CUSTOMER_RISK_CHECKER":
		return "workflow.customer_risk_review"
	case "CUSTOMER_MAKER":
		return "workflow.customer_maker_revise"
	default:
		return ""
	}
}

func (s *ZeebeService) claimTaskForProcess(ctx context.Context, jobType, workerName string, filter TaskClaimFilter, timeout time.Duration) (*WorkflowTask, error) {
	maxAttempts := 10
	if filter.ProcessInstanceKey > 0 || filter.CaseID != "" {
		maxAttempts = 20
	}
	attempts := 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("claim task timed out: %w", err)
		}
		if maxAttempts > 0 && attempts >= maxAttempts {
			break
		}
		callCtx, callCancel := context.WithTimeout(ctx, 5*time.Second)
		jobs, err := s.client.NewActivateJobsCommand().
			JobType(jobType).
			MaxJobsToActivate(1).
			WorkerName(workerName).
			Timeout(timeout).
			FetchVariables(taskFetchVariables...).
			Send(callCtx)
		callCancel()
		if err != nil {
			if isTimeoutErr(err) {
				slog.Warn("zeebe activate jobs call timed out",
					"worker", workerName,
					"jobType", jobType,
					"processInstanceKey", filter.ProcessInstanceKey,
					"caseId", filter.CaseID,
				)
				break
			}
			return nil, fmt.Errorf("claim task: %w", err)
		}
		if len(jobs) == 0 {
			break
		}
		attempts++
		job := jobs[0]
		task := workflowTaskFromJob(job)
		if matchesTaskClaimFilter(task, filter) {
			slog.Info("zeebe claim task matched",
				"worker", workerName,
				"jobType", jobType,
				"jobKey", task.JobKey,
				"processInstanceKey", task.ProcessInstanceKey,
				"elementId", task.ElementID,
				"caseId", task.CaseID,
				"attempts", attempts+1,
			)
			return &task, nil
		}
		if err := s.releaseJob(ctx, job); err != nil {
			return nil, err
		}
	}
	if filter.ProcessInstanceKey > 0 {
		slog.Warn("zeebe claim task exhausted",
			"worker", workerName,
			"jobType", jobType,
			"processInstanceKey", filter.ProcessInstanceKey,
			"caseId", filter.CaseID,
			"elementId", filter.ElementID,
			"attempts", attempts,
		)
		return nil, fmt.Errorf("no task available for job type %q and process instance %d", jobType, filter.ProcessInstanceKey)
	}
	if filter.CaseID != "" {
		slog.Warn("zeebe claim task exhausted",
			"worker", workerName,
			"jobType", jobType,
			"caseId", filter.CaseID,
			"elementId", filter.ElementID,
			"attempts", attempts,
		)
		return nil, fmt.Errorf("no task available for job type %q and case %q", jobType, filter.CaseID)
	}
	slog.Warn("zeebe claim task exhausted",
		"worker", workerName,
		"jobType", jobType,
		"attempts", attempts,
	)
	return nil, fmt.Errorf("no task available for job type %q", jobType)
}

func (s *ZeebeService) releaseJob(ctx context.Context, job entities.Job) error {
	retries := job.GetRetries()
	_, err := s.client.NewFailJobCommand().
		JobKey(job.GetKey()).
		Retries(retries).
		ErrorMessage("skipped: waiting for correlated claim").
		Send(ctx)
	if err != nil {
		return fmt.Errorf("release unrelated job %d: %w", job.GetKey(), err)
	}
	return nil
}

func (s *ZeebeService) CompleteTask(ctx context.Context, jobKey int64, variables map[string]any) error {
	cmd := s.client.NewCompleteJobCommand().JobKey(jobKey)
	if len(variables) > 0 {
		withVars, err := cmd.VariablesFromMap(variables)
		if err != nil {
			return fmt.Errorf("set variables: %w", err)
		}
		_, err = withVars.Send(ctx)
		return err
	}
	_, err := cmd.Send(ctx)
	return err
}

func (s *ZeebeService) NewJobWorker(jobType string, handler func(client worker.JobClient, job entities.Job)) worker.JobWorker {
	return s.client.NewJobWorker().
		JobType(jobType).
		Handler(handler).
		Open()
}

func (s *ZeebeService) NewUserTaskJobWorker(jobType string, handler func(client worker.JobClient, job entities.Job)) worker.JobWorker {
	return s.client.NewJobWorker().
		JobType(jobType).
		Handler(handler).
		Name("arda-workflow-user-task").
		Timeout(30 * time.Minute).
		MaxJobsActive(64).
		Open()
}

var taskFetchVariables = []string{
	"caseId",
	"caseCode",
	"customerId",
	"customerName",
	"riskLevel",
	"reviewDecision",
	"riskDecision",
	"domainService",
	"primaryObjectType",
	"primaryObjectId",
}

func workflowTaskFromJob(job entities.Job) WorkflowTask {
	variables, _ := job.GetVariablesAsMap()
	headers, _ := job.GetCustomHeadersAsMap()
	return WorkflowTask{
		JobKey:             job.GetKey(),
		Type:               job.GetType(),
		ElementID:          job.GetElementId(),
		ProcessInstanceKey: job.GetProcessInstanceKey(),
		CaseID:             strVar(variables, "caseId"),
		CaseCode:           strVar(variables, "caseCode"),
		CustomerID:         firstString(strVar(variables, "customerId"), strVar(variables, "primaryObjectId")),
		CustomerName:       strVar(variables, "customerName"),
		CandidateRole:      headers["candidateRole"],
		FormKey:            headers["formKey"],
		Variables:          variables,
	}
}

func firstString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func strVar(values map[string]any, key string) string {
	if value, ok := values[key].(string); ok {
		return value
	}
	if raw, ok := values[key].(json.Number); ok {
		return raw.String()
	}
	return ""
}
