// Command knowledge-indexer syncs a manifest-managed markdown corpus into the
// AI knowledge base (docs-as-code). See scripts/ai-knowledge/README.md.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge/indexer"
)

func main() {
	manifest := flag.String("manifest", "scripts/ai-knowledge/manifest.yaml", "path to the corpus manifest")
	baseURL := flag.String("base-url", envOr("AI_SERVICE_URL", "http://127.0.0.1:8098"), "ai-service base URL")
	tenant := flag.String("tenant", envOr("AI_CORPUS_TENANT", "00000000-0000-0000-0000-000000000010"), "tenant the corpus pipeline runs in")
	actor := flag.String("actor", indexer.DefaultActor, "review/publish service account (must differ from the manifest owner)")
	permissions := flag.String("permissions", "ai.knowledge.manage,ai.assistant.use", "delegated permissions for the corpus actor")
	dryRun := flag.Bool("dry-run", false, "resolve, scan and report without calling the service")
	jobTimeout := flag.Int("job-timeout-seconds", 120, "per-file ingestion job timeout")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	loaded, err := indexer.LoadManifest(*manifest)
	if err != nil {
		logger.Error("knowledge-indexer: manifest rejected", "err", err)
		os.Exit(1)
	}
	plans, err := indexer.Collect(*manifest, loaded)
	if err != nil {
		logger.Error("knowledge-indexer: sources rejected", "err", err)
		os.Exit(1)
	}

	globalScope := false
	for _, plan := range plans {
		if plan.Entry.Scope != "tenant" {
			globalScope = true
		}
	}

	client := indexer.NewClient(
		strings.TrimRight(strings.TrimSpace(*baseURL), "/"),
		*actor,
		*tenant,
		*permissions,
		globalScope,
		nil,
	)
	client.Cookie = strings.TrimSpace(os.Getenv("AI_CORPUS_COOKIE"))
	opts := indexer.Options{
		DryRun:     *dryRun,
		JobTimeout: time.Duration(*jobTimeout) * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	report, err := indexer.Sync(ctx, client, plans, opts)
	if err != nil {
		// Fail closed: a scan rejection means nothing was published.
		logger.Error("knowledge-indexer: sync aborted", "err", err)
		os.Exit(1)
	}
	logger.Info("knowledge-indexer: sync finished",
		"created", report.Created, "updated", report.Updated, "skipped", report.Skipped,
	)
	for _, failure := range report.Failures {
		logger.Error("knowledge-indexer: file failed", "path", failure.Path, "reason", failure.Reason)
	}
	if report.Failed() {
		os.Exit(1)
	}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
