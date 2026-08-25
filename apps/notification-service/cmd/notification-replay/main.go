package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"github.com/arda-labs/arda/apps/notification-service/internal/repository"
)

const confirmation = "REPLAY_ONE"

func main() {
	outboxID := flag.String("outbox-id", "", "one dead-lettered outbox ID to replay")
	operator := flag.String("operator", "", "audited operator identity")
	confirm := flag.String("confirm", "", "must be REPLAY_ONE")
	flag.Parse()

	if err := validateInputs(*outboxID, *operator, *confirm, os.Getenv("DATABASE_DSN")); err != nil {
		fmt.Fprintf(os.Stderr, "usage: notification-replay --outbox-id ID --operator OPERATOR --confirm %s\n", confirmation)
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(2)
	}
	dsn := strings.TrimSpace(os.Getenv("DATABASE_DSN"))

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "ping database: %v\n", err)
		os.Exit(1)
	}

	if err := repository.NewNotificationRepository(db).ReplayOutboxDLQ(ctx, strings.TrimSpace(*outboxID), strings.TrimSpace(*operator)); err != nil {
		fmt.Fprintf(os.Stderr, "replay one outbox event: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("replayed one notification outbox event: outbox_id=%s operator=%s\n", strings.TrimSpace(*outboxID), strings.TrimSpace(*operator))
}

func validateInputs(outboxID, operator, confirm, dsn string) error {
	if strings.TrimSpace(outboxID) == "" {
		return fmt.Errorf("outbox ID is required")
	}
	if strings.TrimSpace(operator) == "" {
		return fmt.Errorf("operator identity is required")
	}
	if confirm != confirmation {
		return fmt.Errorf("confirmation must be %s", confirmation)
	}
	if strings.TrimSpace(dsn) == "" {
		return fmt.Errorf("DATABASE_DSN is required")
	}
	return nil
}
