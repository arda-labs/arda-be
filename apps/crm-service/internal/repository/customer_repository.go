package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type Customer struct {
	ID        string
	Name      string
	Email     string
	Status    string
	CreatedAt time.Time
}

type CustomerRepository struct {
	db *sql.DB
}

func NewCustomerRepository(db *sql.DB) *CustomerRepository {
	return &CustomerRepository{db: db}
}

func (r *CustomerRepository) Create(ctx context.Context, id, name, email, status string) error {
	query := `
		INSERT INTO customers (id, name, email, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	_, err := r.db.ExecContext(ctx, query, id, name, email, status, time.Now())
	return err
}

func (r *CustomerRepository) Get(ctx context.Context, id string) (*Customer, error) {
	query := `
		SELECT id, name, email, status, created_at
		FROM customers
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	var c Customer
	err := row.Scan(&c.ID, &c.Name, &c.Email, &c.Status, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *CustomerRepository) UpdateStatus(ctx context.Context, id, status string) error {
	query := `
		UPDATE customers
		SET status = $2
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id, status)
	return err
}

func (r *CustomerRepository) Update(ctx context.Context, id, name, email string) error {
	query := `
		UPDATE customers
		SET name = $2, email = $3
		WHERE id = $1
	`
	_, err := r.db.ExecContext(ctx, query, id, name, email)
	return err
}
