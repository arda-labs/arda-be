package repository

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"
)

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func (r *CustomerRepository) nextTempCustomerCode(ctx context.Context, customerType string) (string, error) {
	prefix := "DKKH-T-"
	if customerType == "BUSINESS" {
		prefix = "DKKH-O-"
	}
	var seq int64
	if err := r.db.QueryRowContext(ctx, `SELECT nextval('crm_customer_temp_code_seq')`).Scan(&seq); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s-%06d", prefix, time.Now().UTC().Format("20060102"), seq), nil
}

func (r *CustomerRepository) nextOfficialCustomerCode(ctx context.Context, customerType string) (string, error) {
	prefix := "DKKH-T-"
	if customerType == "BUSINESS" {
		prefix = "DKKH-O-"
	}
	var seq int64
	if err := r.db.QueryRowContext(ctx, `SELECT nextval('crm_customer_official_code_seq')`).Scan(&seq); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s-%06d", prefix, time.Now().UTC().Format("20060102"), seq), nil
}
