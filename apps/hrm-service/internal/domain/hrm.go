package domain

import "time"

type Position struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	IsManager   bool      `json:"is_manager"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type JobTitle struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type OrgUnit struct {
	ID               string    `json:"id"`
	Code             string    `json:"code"`
	OrganizationID   string    `json:"organization_id"`
	OrganizationCode *string   `json:"organization_code,omitempty"`
	OrganizationName *string   `json:"organization_name,omitempty"`
	Name             string    `json:"name"`
	OrgLevel         string    `json:"org_level"`
	ParentID         *string   `json:"parent_id,omitempty"`
	DepartmentType   string    `json:"department_type"`
	Status           string    `json:"status"`
	Description      *string   `json:"description,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Employee struct {
	ID           string    `json:"id"`
	EmployeeCode string    `json:"employee_code"`
	FullName     string    `json:"full_name"`
	OrgUnitID    *string   `json:"org_unit_id,omitempty"`
	PositionID   *string   `json:"position_id,omitempty"`
	JobTitleID   *string   `json:"job_title_id,omitempty"`
	IAMUserID    *string   `json:"iam_user_id,omitempty"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type EmployeeRegistration struct {
	ID               string    `json:"id"`
	RegistrationCode string    `json:"registration_code"`
	Payload          string    `json:"payload"`
	WorkflowCaseID   *string   `json:"workflow_case_id,omitempty"`
	Status           string    `json:"status"`
	CreatedBy        *string   `json:"created_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
