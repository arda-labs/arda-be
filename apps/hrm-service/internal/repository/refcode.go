package repository

import (
	"context"
	"fmt"
	"time"
)

func (r *HRMRepository) nextRegistrationCode(ctx context.Context) (string, error) {
	var seq int64
	if err := r.db.QueryRowContext(ctx, `SELECT nextval('hrm_registration_code_seq')`).Scan(&seq); err != nil {
		return "", err
	}
	return fmt.Sprintf("DKNV-T-%s-%06d", time.Now().UTC().Format("20060102"), seq), nil
}
