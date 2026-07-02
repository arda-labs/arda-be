package repository

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

type ProcessDefinition struct {
	ID            string     `json:"id"`
	ProcessCode   string     `json:"processCode"`
	Name          string     `json:"name"`
	BpmnProcessID string     `json:"bpmnProcessId"`
	Version       int        `json:"version"`
	ResourceName  string     `json:"resourceName"`
	XMLContent    string     `json:"xmlContent,omitempty"`
	DeploymentKey *int64     `json:"deploymentKey,omitempty"`
	Status        string     `json:"status"`
	DeployedAt    *time.Time `json:"deployedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type ProcessDefinitionImport struct {
	ProcessCode  string
	Name         string
	ResourceName string
	XMLContent   string
	Status       string
}

type ProcessDefinitionRepository struct {
	db *sql.DB
}

func NewProcessDefinitionRepository(db *sql.DB) *ProcessDefinitionRepository {
	return &ProcessDefinitionRepository{db: db}
}

func (r *ProcessDefinitionRepository) List(ctx context.Context) ([]ProcessDefinition, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, process_code, name, bpmn_process_id, version, resource_name, '',
		       deployment_key, status, deployed_at, created_at, updated_at
		FROM workflow_process_definitions
		ORDER BY updated_at DESC, process_code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProcessDefinition
	for rows.Next() {
		item, err := scanProcessDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *ProcessDefinitionRepository) Get(ctx context.Context, id string) (*ProcessDefinition, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, process_code, name, bpmn_process_id, version, resource_name, xml_content,
		       deployment_key, status, deployed_at, created_at, updated_at
		FROM workflow_process_definitions
		WHERE id = $1
	`, id)
	item, err := scanProcessDefinition(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *ProcessDefinitionRepository) Create(ctx context.Context, in ProcessDefinitionImport) (*ProcessDefinition, error) {
	bpmnProcessID, err := validateProcessDefinitionImport(in, true)
	if err != nil {
		return nil, err
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}

	row := r.db.QueryRowContext(ctx, `
		INSERT INTO workflow_process_definitions (
			id, process_code, name, bpmn_process_id, resource_name, xml_content, status
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, process_code, name, bpmn_process_id, version, resource_name, xml_content,
		          deployment_key, status, deployed_at, created_at, updated_at
	`, id, in.ProcessCode, in.Name, bpmnProcessID, in.ResourceName, in.XMLContent, statusOrDraft(in.Status))
	item, err := scanProcessDefinition(row)
	return &item, err
}

func (r *ProcessDefinitionRepository) Update(ctx context.Context, id string, in ProcessDefinitionImport) (*ProcessDefinition, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	bpmnProcessID, err := validateProcessDefinitionImport(in, false)
	if err != nil {
		return nil, err
	}

	row := r.db.QueryRowContext(ctx, `
		UPDATE workflow_process_definitions
		SET name = $2,
		    bpmn_process_id = $3,
		    version = version + 1,
		    resource_name = $4,
		    xml_content = $5,
		    deployment_key = NULL,
		    status = $6,
		    deployed_at = NULL,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, process_code, name, bpmn_process_id, version, resource_name, xml_content,
		          deployment_key, status, deployed_at, created_at, updated_at
	`, id, in.Name, bpmnProcessID, in.ResourceName, in.XMLContent, statusOrDraft(in.Status))
	item, err := scanProcessDefinition(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func (r *ProcessDefinitionRepository) MarkDeployed(ctx context.Context, id string, deploymentKey int64) (*ProcessDefinition, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE workflow_process_definitions
		SET deployment_key = $2,
		    status = 'ACTIVE',
		    deployed_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id, process_code, name, bpmn_process_id, version, resource_name, xml_content,
		          deployment_key, status, deployed_at, created_at, updated_at
	`, id, deploymentKey)
	item, err := scanProcessDefinition(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &item, err
}

func (r *ProcessDefinitionRepository) Delete(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, errors.New("id is required")
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM workflow_process_definitions WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func validateProcessDefinitionImport(in ProcessDefinitionImport, requireCode bool) (string, error) {
	switch {
	case requireCode && strings.TrimSpace(in.ProcessCode) == "":
		return "", errors.New("processCode is required")
	case strings.TrimSpace(in.Name) == "":
		return "", errors.New("name is required")
	case strings.TrimSpace(in.ResourceName) == "":
		return "", errors.New("resourceName is required")
	case strings.TrimSpace(in.XMLContent) == "":
		return "", errors.New("xmlContent is required")
	}
	bpmnProcessID, err := ExtractBPMNProcessID([]byte(in.XMLContent))
	if err != nil {
		return "", err
	}
	return bpmnProcessID, nil
}

func ExtractBPMNProcessID(content []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(content))
	seenDefinitions := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("invalid BPMN XML: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Local == "definitions" {
			seenDefinitions = true
		}
		if start.Name.Local != "process" {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local == "id" && strings.TrimSpace(attr.Value) != "" {
				return strings.TrimSpace(attr.Value), nil
			}
		}
		return "", errors.New("BPMN process id is required")
	}
	if !seenDefinitions {
		return "", errors.New("BPMN definitions element is required")
	}
	return "", errors.New("BPMN process element is required")
}

func scanProcessDefinition(s scanner) (ProcessDefinition, error) {
	var item ProcessDefinition
	var deploymentKey sql.NullInt64
	var deployedAt sql.NullTime
	err := s.Scan(&item.ID, &item.ProcessCode, &item.Name, &item.BpmnProcessID, &item.Version,
		&item.ResourceName, &item.XMLContent, &deploymentKey, &item.Status, &deployedAt,
		&item.CreatedAt, &item.UpdatedAt)
	if deploymentKey.Valid {
		item.DeploymentKey = &deploymentKey.Int64
	}
	if deployedAt.Valid {
		item.DeployedAt = &deployedAt.Time
	}
	return item, err
}

func statusOrDraft(status string) string {
	if status == "" {
		return "DRAFT"
	}
	return status
}
