// knowledge-indexer implements the docs-as-code ingestion pipeline for AI
// knowledge (arda-be/docs/ai/agent-evolution-roadmap.md §4.6). Run it from CI
// on merge (or manually) against a markdown directory:
//
//	knowledge-indexer -dir docs/product -version $(git rev-parse --short HEAD) -scope global
//
// Only changed chunks are re-embedded; unchanged chunks keep their vectors.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/lib/pq"

	"github.com/arda-labs/arda/apps/ai-service/internal/knowledge"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	dir := flag.String("dir", "", "markdown directory to index (walked recursively)")
	version := flag.String("version", "", "source version, e.g. the git commit SHA")
	tenant := flag.String("tenant", "", "tenant ID for tenant-scoped docs; empty = global")
	sourceType := flag.String("type", "docs", "source_type recorded on the knowledge sources")
	dsn := flag.String("dsn", os.Getenv("DATABASE_DSN"), "Postgres DSN (defaults to DATABASE_DSN)")
	embeddingProvider := flag.String("embedding-provider", envOr("AI_EMBEDDING_PROVIDER", "workersai"), "workersai | openai")
	embeddingModel := flag.String("embedding-model", envOr("AI_EMBEDDING_MODEL", "@cf/qwen/qwen3-embedding-0.6b"), "embedding model identifier")
	embeddingToken := flag.String("embedding-token", os.Getenv("AI_EMBEDDING_API_TOKEN"), "embedding API token")
	embeddingAccount := flag.String("embedding-account", os.Getenv("AI_EMBEDDING_ACCOUNT_ID"), "Cloudflare account ID (workersai)")
	embeddingBaseURL := flag.String("embedding-base-url", os.Getenv("AI_EMBEDDING_BASE_URL"), "OpenAI-compatible base URL (openai provider)")
	flag.Parse()

	if *dir == "" || *version == "" {
		fmt.Fprintln(os.Stderr, "usage: knowledge-indexer -dir <markdown-dir> -version <git-sha>")
		os.Exit(2)
	}
	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_DSN is required (or pass -dsn)")
		os.Exit(2)
	}

	db, err := sql.Open("postgres", *dsn)
	if err != nil {
		logger.Error("open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	if err := db.PingContext(context.Background()); err != nil {
		logger.Error("ping database", "err", err)
		os.Exit(1)
	}

	var embedder knowledge.Embedder
	switch strings.ToLower(*embeddingProvider) {
	case "openai":
		if *embeddingBaseURL != "" && *embeddingModel != "" {
			embedder = knowledge.NewOpenAIEmbedder(*embeddingBaseURL, *embeddingModel, *embeddingToken, nil)
		}
	default:
		if *embeddingAccount != "" && *embeddingToken != "" && *embeddingModel != "" {
			embedder = knowledge.NewWorkersAIEmbedder(*embeddingAccount, *embeddingModel, *embeddingToken, nil)
		}
	}
	if embedder == nil {
		logger.Warn("embedding not configured; indexing text chunks without vectors")
	}

	indexer := knowledge.NewIndexer(db, embedder)
	ctx := context.Background()

	totalEmbedded, totalSkipped, totalFiles := 0, 0, 0
	err = filepath.WalkDir(*dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			return walkErr
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relPath, relErr := filepath.Rel(*dir, path)
		if relErr != nil {
			return relErr
		}
		sourceKey := filepath.ToSlash(relPath)
		title := firstHeading(string(content), sourceKey)

		embedded, skipped, indexErr := indexer.IndexDocument(ctx, *tenant, sourceKey, *version, title, string(content), *sourceType)
		if indexErr != nil {
			logger.Error("index document", "source", sourceKey, "err", indexErr)
			return indexErr
		}
		totalFiles++
		totalEmbedded += embedded
		totalSkipped += skipped
		logger.Info("indexed", "source", sourceKey, "embedded", embedded, "skipped", skipped)
		return nil
	})
	if err != nil {
		os.Exit(1)
	}
	logger.Info("indexing complete", "files", totalFiles, "embedded", totalEmbedded, "unchanged", totalSkipped, "version", *version)
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func firstHeading(markdown, fallback string) string {
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return fallback
}
