package domain

import (
	"errors"
	"time"
)

// SystemDate status types.
const (
	SystemDateOpen          = "OPEN"
	SystemDateEODProcessing = "EOD_PROCESSING"
	SystemDateClosed        = "CLOSED"
)

// Business-date claim errors. They live in the domain so the repository
// (conditional UPDATE + rows-affected) and the HTTP layer can classify the
// same failure without importing each other.
var (
	// ErrSystemDateNotFound means plt_system_dates has no row for the branch.
	ErrSystemDateNotFound = errors.New("system date config not found")
	// ErrEODInProgress means another EOD run already holds EOD_PROCESSING.
	ErrEODInProgress = errors.New("EOD process is already in progress")
)

// SystemDate tracks the business calendar state.
type SystemDate struct {
	ID                   string     `json:"id"`
	BranchCode           string     `json:"branch_code"`
	CurrentBusinessDate  time.Time  `json:"current_business_date"`
	PreviousBusinessDate time.Time  `json:"previous_business_date"`
	NextBusinessDate     time.Time  `json:"next_business_date"`
	Status               string     `json:"status"`
	LastEODAt            *time.Time `json:"last_eod_at,omitempty"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// HolidayCalendar represents a holiday/non-working day.
type HolidayCalendar struct {
	ID          string    `json:"id"`
	HolidayDate time.Time `json:"holiday_date"`
	Description string    `json:"description"`
	IsRecurring bool      `json:"is_recurring"`
	HolidayYear *int      `json:"holiday_year,omitempty"` // Nullable, populated if is_recurring = false
	CreatedAt   time.Time `json:"created_at"`
}

// CutoffConfig models transaction cut-off times.
type CutoffConfig struct {
	ID              string    `json:"id"`
	ChannelCode     string    `json:"channel_code"`
	TransactionType string    `json:"transaction_type"`
	CutoffTime      string    `json:"cutoff_time"` // format: "15:04:05" or similar
	IsActive        bool      `json:"is_active"`
	UpdatedAt       time.Time `json:"updated_at"`
}
