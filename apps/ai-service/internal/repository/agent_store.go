package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

type AgentConfig struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenantId"`
	Name         string    `json:"name"`
	Department   string    `json:"department"`
	Description  string    `json:"description"`
	SystemPrompt string    `json:"systemPrompt"`
	ModelID      string    `json:"modelId"`
	Temperature  float32   `json:"temperature"`
	AllowedTools []string  `json:"allowedTools"`
	IsActive     bool      `json:"isActive"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type AgentStore interface {
	ListAgents(ctx context.Context, tenantID string) ([]AgentConfig, error)
	SaveAgent(ctx context.Context, agent AgentConfig) (*AgentConfig, error)
	DeleteAgent(ctx context.Context, tenantID, agentID string) error
}

func DefaultAgents(tenantID string) []AgentConfig {
	now := time.Now()
	return []AgentConfig{
		{
			ID:           "hr-assistant",
			TenantID:     tenantID,
			Name:         "HR Assistant",
			Department:   "HR",
			Description:  "Trợ lý chuyên trách tra cứu hồ sơ nhân viên, quy chế đãi ngộ và chính sách nội bộ.",
			SystemPrompt: "Bạn là Trợ lý Nhân sự chuyên trách tra cứu thông tin nhân viên, hợp đồng và chính sách nhân sự của doanh nghiệp. Luôn trả lời chính xác, bảo mật và chuẩn mực.",
			ModelID:      "gemini-2.5-flash",
			Temperature:  0.2,
			AllowedTools: []string{"arda.hrm.listEmployees", "arda.knowledge.search"},
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			ID:           "sales-specialist",
			TenantID:     tenantID,
			Name:         "Sales & CRM Specialist",
			Department:   "Sales",
			Description:  "Chuyên viên hỗ trợ thông tin khách hàng, lịch sử giao dịch và phân khúc đối tác.",
			SystemPrompt: "Bạn là Chuyên viên Hỗ trợ Kinh doanh, nắm vững thông tin khách hàng, phân khúc tiềm năng và đề xuất kịch bản chăm sóc khách hàng tối ưu.",
			ModelID:      "gemini-2.5-flash",
			Temperature:  0.3,
			AllowedTools: []string{"arda.crm.getCustomer", "arda.knowledge.search"},
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			ID:           "finance-analyst",
			TenantID:     tenantID,
			Name:         "Financial Analyst",
			Department:   "Finance",
			Description:  "Chuyên viên phân tích tài chính, tra cứu hệ thống tài khoản kế toán và số dư sổ cái.",
			SystemPrompt: "Bạn là Chuyên viên Phân tích Kế toán - Tài chính. Bạn hỗ trợ tra cứu hệ thống tài khoản, kiểm tra số dư và giải thích báo cáo tài chính một cách thận trọng và chuẩn xác.",
			ModelID:      "gemini-2.5-flash",
			Temperature:  0.1,
			AllowedTools: []string{"arda.finance.getAccount"},
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			ID:           "tech-support",
			TenantID:     tenantID,
			Name:         "IT & DevOps Support",
			Department:   "Tech",
			Description:  "Kỹ sư hỗ trợ kỹ thuật, kiểm tra quyền hạn IAM, cấu hình hệ thống và hạ tầng.",
			SystemPrompt: "Bạn là Kỹ sư Hỗ trợ Kỹ thuật IT và Hạ tầng. Bạn hỗ trợ giải đáp thắc mắc về phân quyền IAM, trạng thái dịch vụ và hướng dẫn xử lý sự cố.",
			ModelID:      "qwen2.5:7b-instruct-q4_K_M",
			Temperature:  0.2,
			AllowedTools: []string{"arda.iam.getScope"},
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		{
			ID:           "general-assistant",
			TenantID:     tenantID,
			Name:         "Olorin General Assistant",
			Department:   "General",
			Description:  "Trợ lý điều hành đa nhiệm thông minh, kết nối toàn diện với tất cả công cụ hệ thống.",
			SystemPrompt: "Bạn là Olorin, Trợ lý AI trung tâm của hệ điều hành doanh nghiệp Arda. Bạn có khả năng phối hợp đa công cụ để giải quyết bài toán của người dùng.",
			ModelID:      "gemini-2.5-flash",
			Temperature:  0.4,
			AllowedTools: []string{"*"},
			IsActive:     true,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
	}
}

func (s *SQLRunStore) ListAgents(ctx context.Context, tenantID string) ([]AgentConfig, error) {
	if s == nil || s.db == nil {
		return DefaultAgents(tenantID), nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, name, department, coalesce(description, ''), system_prompt,
		       model_id, temperature, allowed_tools, is_active, created_at, updated_at
		FROM public.ai_agents
		WHERE tenant_id = $1 OR tenant_id = ''
		ORDER BY department ASC, name ASC
	`, tenantID)
	if err != nil {
		// If table does not exist or error, fallback to defaults
		return DefaultAgents(tenantID), nil
	}
	defer rows.Close()

	agents := make([]AgentConfig, 0)
	for rows.Next() {
		var a AgentConfig
		var tools pq.StringArray
		if err := rows.Scan(
			&a.ID, &a.TenantID, &a.Name, &a.Department, &a.Description, &a.SystemPrompt,
			&a.ModelID, &a.Temperature, &tools, &a.IsActive, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan agent config: %w", err)
		}
		a.AllowedTools = []string(tools)
		agents = append(agents, a)
	}

	if len(agents) == 0 {
		return DefaultAgents(tenantID), nil
	}
	return agents, nil
}

func (s *SQLRunStore) SaveAgent(ctx context.Context, agent AgentConfig) (*AgentConfig, error) {
	if s == nil || s.db == nil {
		return &agent, nil
	}
	agent.Name = strings.TrimSpace(agent.Name)
	if agent.Name == "" {
		return nil, fmt.Errorf("agent name is required")
	}
	if strings.TrimSpace(agent.ID) == "" {
		agent.ID = strings.ToLower(strings.ReplaceAll(agent.Name, " ", "-"))
	}
	if agent.Department == "" {
		agent.Department = "General"
	}
	if agent.ModelID == "" {
		agent.ModelID = "gemini-2.5-flash"
	}
	if agent.Temperature <= 0 || agent.Temperature > 2 {
		agent.Temperature = 0.2
	}
	now := time.Now()
	agent.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO public.ai_agents
			(id, tenant_id, name, department, description, system_prompt, model_id, temperature, allowed_tools, is_active, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
		ON CONFLICT (tenant_id, name) DO UPDATE SET
			department = EXCLUDED.department,
			description = EXCLUDED.description,
			system_prompt = EXCLUDED.system_prompt,
			model_id = EXCLUDED.model_id,
			temperature = EXCLUDED.temperature,
			allowed_tools = EXCLUDED.allowed_tools,
			is_active = EXCLUDED.is_active,
			updated_at = EXCLUDED.updated_at
	`, agent.ID, agent.TenantID, agent.Name, agent.Department, agent.Description, agent.SystemPrompt,
		agent.ModelID, agent.Temperature, pq.Array(agent.AllowedTools), agent.IsActive, now)
	if err != nil {
		return nil, fmt.Errorf("save agent: %w", err)
	}
	return &agent, nil
}

func (s *SQLRunStore) DeleteAgent(ctx context.Context, tenantID, agentID string) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM public.ai_agents
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, agentID)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	return nil
}
