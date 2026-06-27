package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type ProcessMapping struct {
	BusinessKey        string
	ProcessInstanceKey int64
	BpmnProcessID      string
	Status             string
	CreatedAt          time.Time
}

type MappingRepository struct {
	db *sql.DB
}

func NewMappingRepository(db *sql.DB) *MappingRepository {
	return &MappingRepository{db: db}
}

func (r *MappingRepository) SaveMapping(ctx context.Context, businessKey string, processInstanceKey int64, bpmnProcessID string, status string) error {
	query := `
		INSERT INTO process_mappings (business_key, process_instance_key, bpmn_process_id, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (business_key) DO UPDATE
		SET process_instance_key = EXCLUDED.process_instance_key,
		    bpmn_process_id = EXCLUDED.bpmn_process_id,
		    status = EXCLUDED.status
	`
	_, err := r.db.ExecContext(ctx, query, businessKey, processInstanceKey, bpmnProcessID, status, time.Now())
	return err
}

func (r *MappingRepository) GetMapping(ctx context.Context, businessKey string) (*ProcessMapping, error) {
	query := `
		SELECT business_key, process_instance_key, bpmn_process_id, status, created_at
		FROM process_mappings
		WHERE business_key = $1
	`
	row := r.db.QueryRowContext(ctx, query, businessKey)
	var m ProcessMapping
	err := row.Scan(&m.BusinessKey, &m.ProcessInstanceKey, &m.BpmnProcessID, &m.Status, &m.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *MappingRepository) UpdateMappingStatus(ctx context.Context, businessKey string, status string) error {
	query := `
		UPDATE process_mappings
		SET status = $2
		WHERE business_key = $1
	`
	_, err := r.db.ExecContext(ctx, query, businessKey, status)
	return err
}
