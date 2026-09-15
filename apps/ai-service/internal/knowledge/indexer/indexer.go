// Package indexer implements the docs-as-code knowledge pipeline: it reads a
// manifest of markdown sources, fails closed on secret/PII patterns, and syncs
// every file into the AI knowledge base through the public /api/rag/* surface
// (create → version → review → publish → wait for the ingestion job).
//
// Identity model: the CLI signs requests as a trusted workload (source
// "auth-gateway", audience "ai-service") and impersonates the corpus service
// accounts via the gateway's delegated headers, mirroring
// scripts/ai-dev-corpus/seed.mjs. The corpus reviewer (actor) must differ from
// the manifest owner so the repository's self-review guard passes.
package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/arda-labs/arda/libs/go/arda-grpc/identity"
	"gopkg.in/yaml.v3"
)

// Default service accounts for the docs-as-code pipeline. The reviewer must
// never equal the owner: CreateSource records the owner, ReviewVersion blocks
// self-review, so auto-approval is auditable and structurally distinct.
const (
	DefaultOwner   = "docs-team"
	DefaultActor   = "corpus-bot"
	DefaultScope   = "global"
	DocsAsCodeTag  = "corpus:docs-as-code"
	signAudience   = "ai-service"
	signSource     = "auth-gateway"
	defaultTimeout = 120 * time.Second
)

// Manifest is scripts/ai-knowledge/manifest.yaml.
type Manifest struct {
	Corpus []CorpusEntry `yaml:"corpus"`
}

type CorpusEntry struct {
	Name           string       `yaml:"name"`
	Description    string       `yaml:"description"`
	Scope          string       `yaml:"scope"`
	Classification string       `yaml:"classification"`
	Language       string       `yaml:"language"`
	Owner          string       `yaml:"owner"`
	Tags           []string     `yaml:"tags"`
	Sources        []SourceGlob `yaml:"sources"`
}

type SourceGlob struct {
	Glob string `yaml:"glob"`
}

// FilePlan is one markdown file resolved from a manifest glob.
type FilePlan struct {
	// RelPath is the stable source identity and title (slash-separated,
	// relative to the manifest directory).
	RelPath string
	AbsPath string
	Content string
	Hash    string // sha256 hex of Content
}

type SourcePlan struct {
	Entry CorpusEntry
	Files []FilePlan
}

// LoadManifest parses and validates the manifest. Missing optional fields get
// pipeline defaults; nothing is mutated silently afterwards.
func LoadManifest(path string) (*Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if len(manifest.Corpus) == 0 {
		return nil, fmt.Errorf("manifest %s declares no corpus entries", path)
	}
	for i := range manifest.Corpus {
		entry := &manifest.Corpus[i]
		entry.Name = strings.TrimSpace(entry.Name)
		if entry.Name == "" {
			return nil, fmt.Errorf("manifest: corpus entry #%d has no name", i+1)
		}
		entry.Scope = strings.ToLower(strings.TrimSpace(entry.Scope))
		if entry.Scope == "" {
			entry.Scope = DefaultScope
		}
		if entry.Scope != "tenant" && entry.Scope != "global" && entry.Scope != "system" {
			return nil, fmt.Errorf("manifest: %s has invalid scope %q (tenant|global|system)", entry.Name, entry.Scope)
		}
		if strings.TrimSpace(entry.Classification) == "" {
			entry.Classification = "internal"
		}
		if strings.TrimSpace(entry.Language) == "" {
			entry.Language = "vi"
		}
		if strings.TrimSpace(entry.Owner) == "" {
			entry.Owner = DefaultOwner
		}
		if len(entry.Sources) == 0 {
			return nil, fmt.Errorf("manifest: %s declares no sources", entry.Name)
		}
	}
	return &manifest, nil
}

var contentExtensions = map[string]bool{
	".md": true, ".markdown": true, ".txt": true,
}

// Collect expands every manifest glob relative to the manifest directory and
// loads the file contents. Unsupported extensions fail closed instead of being
// silently indexed as raw text; a glob matching nothing is an error so stale
// manifests stay visible.
func Collect(manifestPath string, manifest *Manifest) ([]SourcePlan, error) {
	root, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return nil, err
	}
	var plans []SourcePlan
	for _, entry := range manifest.Corpus {
		plan := SourcePlan{Entry: entry}
		for _, source := range entry.Sources {
			if strings.TrimSpace(source.Glob) == "" {
				return nil, fmt.Errorf("manifest: %s has an empty source glob", entry.Name)
			}
			matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(source.Glob)))
			if err != nil {
				return nil, fmt.Errorf("manifest: %s glob %q: %w", entry.Name, source.Glob, err)
			}
			if len(matches) == 0 {
				return nil, fmt.Errorf("manifest: %s glob %q matched no files", entry.Name, source.Glob)
			}
			sort.Strings(matches)
			for _, abs := range matches {
				info, err := os.Stat(abs)
				if err != nil {
					return nil, fmt.Errorf("stat %s: %w", abs, err)
				}
				if info.IsDir() {
					continue
				}
				if ext := strings.ToLower(filepath.Ext(abs)); !contentExtensions[ext] {
					return nil, fmt.Errorf("%s: extension %q is not indexable (md/markdown/txt only)", abs, ext)
				}
				raw, err := os.ReadFile(abs)
				if err != nil {
					return nil, fmt.Errorf("read %s: %w", abs, err)
				}
				rel, err := filepath.Rel(root, abs)
				if err != nil {
					return nil, fmt.Errorf("resolve %s: %w", abs, err)
				}
				sum := sha256.Sum256(raw)
				plan.Files = append(plan.Files, FilePlan{
					RelPath: filepath.ToSlash(rel),
					AbsPath: abs,
					Content: string(raw),
					Hash:    hex.EncodeToString(sum[:]),
				})
			}
		}
		if len(plan.Files) == 0 {
			return nil, fmt.Errorf("manifest: %s resolved no files", entry.Name)
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// Secret/PII deny-list. Auto-approved corpus content must never carry
// credentials or personal identifiers, so a single match blocks the whole sync.
var scanPatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9]{16,}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|passwd|password)\b\s*[:=]\s*\S{8,}`),
	regexp.MustCompile(`\b0\d{11}\b`), // 12-digit VN citizen ID (CCCD) shape
}

// Scan returns human-readable findings (line + reason). Empty result means the
// content is publishable by the pipeline.
func Scan(content string) []string {
	var findings []string
	for i, line := range strings.Split(content, "\n") {
		for _, pattern := range scanPatterns {
			if pattern.MatchString(line) {
				findings = append(findings, fmt.Sprintf("line %d: matches %s", i+1, pattern.String()))
				break
			}
		}
	}
	return findings
}

// Client talks to a deployed ai-service with a signed workload identity plus
// the delegated subject headers the gateway normally injects.
type Client struct {
	BaseURL     string // no trailing slash
	Actor       string // reviewer/publisher service account
	Tenant      string
	Permissions string // comma-separated, must include ai.knowledge.manage
	GlobalAdmin bool
	// Secret overrides the workload signing secret; empty reads
	// ARDA_SERVICE_AUTH_SECRET (tests inject a fixture instead).
	Secret string
	// Cookie carries the corpus service-account session when the pipeline runs
	// against the public gateway (api.arda.io.vn) instead of an in-cluster
	// ai-service. Empty for direct in-cluster calls.
	Cookie string
	HTTP   *http.Client
}

func NewClient(baseURL, actor, tenant, permissions string, globalAdmin bool, hc *http.Client) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Actor:       actor,
		Tenant:      tenant,
		Permissions: permissions,
		GlobalAdmin: globalAdmin,
		HTTP:        hc,
	}
}

type apiError struct {
	Status int
	Code   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("ai-service %d: %s", e.Status, e.Code)
}

// secretFromEnv is a var for test injection; production reads the shared
// ARDA_SERVICE_AUTH_SECRET that signs ai-service workload identity.
var secretFromEnv = os.Getenv

// request signs and sends one API call, always with the delegated headers.
func (c *Client) request(ctx context.Context, method, path string, body any, out any) error {
	if c.BaseURL == "" {
		return fmt.Errorf("ai-service base URL is not configured")
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(encoded))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	secret := c.Secret
	if secret == "" {
		secret = secretFromEnv("ARDA_SERVICE_AUTH_SECRET")
	}
	if len(secret) < 32 {
		return fmt.Errorf("workload signing secret is required (>=32 chars) to sign ai-service identity")
	}
	if err := identity.SignRequest(req, secret, signSource, signAudience, time.Now(), 5*time.Minute); err != nil {
		return fmt.Errorf("sign request: %w", err)
	}
	req.Header.Set("X-Auth-Checked", "true")
	req.Header.Set("X-User-Id", c.Actor)
	req.Header.Set("X-Tenant-Id", c.Tenant)
	req.Header.Set("X-Permissions", c.Permissions)
	if c.GlobalAdmin {
		req.Header.Set("X-Global-Admin", "true")
		req.Header.Set("X-Global-Roles", "SUPER_ADMIN")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(c.Cookie) != "" {
		req.Header.Set("Cookie", strings.TrimSpace(c.Cookie))
	}

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		code := strings.TrimSpace(string(raw))
		var problem struct {
			Code string `json:"code"`
		}
		if json.Unmarshal(raw, &problem) == nil && problem.Code != "" {
			code = problem.Code
		}
		return &apiError{Status: resp.StatusCode, Code: code}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}

// Options tunes a Sync run.
type Options struct {
	DryRun bool
	// JobTimeout bounds waiting for each ingestion job; a timed-out job is a
	// failure, never a silent skip.
	JobTimeout time.Duration
}

type Report struct {
	Created  []string
	Updated  []string
	Skipped  []string
	Failures []Failure
}

type Failure struct {
	Path   string
	Reason string
}

func (r *Report) Failed() bool { return len(r.Failures) > 0 }

type apiSource struct {
	ID        int64    `json:"id"`
	Title     string   `json:"title"`
	Scope     string   `json:"scope"`
	OwnerID   *string  `json:"owner_id"`
	Tags      []string `json:"tags"`
	TenantID  *string  `json:"tenant_id"`
	DeletedAt *string  `json:"deleted_at"`
}

type apiVersion struct {
	ID          int64   `json:"id"`
	Status      string  `json:"status"`
	ContentHash *string `json:"content_hash"`
}

type apiJob struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Sync runs the whole pipeline for every plan. It fails closed: a scan finding
// aborts the run before any API call, and per-file API errors are collected
// into Report.Failures for the caller to surface with a non-zero exit.
func Sync(ctx context.Context, client *Client, plans []SourcePlan, opts Options) (Report, error) {
	report := Report{}
	if opts.JobTimeout <= 0 {
		opts.JobTimeout = defaultTimeout
	}

	// Fail closed before touching the API: one leaky document must not be
	// partially published while the rest continue.
	for _, plan := range plans {
		for _, file := range plan.Files {
			if findings := Scan(file.Content); len(findings) > 0 {
				return report, fmt.Errorf(
					"scan rejected %s (%s): %s",
					file.RelPath, plan.Entry.Name, strings.Join(findings, "; "),
				)
			}
		}
	}
	if opts.DryRun {
		for _, plan := range plans {
			for _, file := range plan.Files {
				report.Skipped = append(report.Skipped, file.RelPath+" (dry-run)")
			}
		}
		return report, nil
	}

	for _, plan := range plans {
		for _, file := range plan.Files {
			outcome, err := syncFile(ctx, client, plan.Entry, file, opts)
			switch {
			case err != nil:
				report.Failures = append(report.Failures, Failure{Path: file.RelPath, Reason: err.Error()})
			case outcome.skipped:
				report.Skipped = append(report.Skipped, file.RelPath)
			case outcome.created:
				report.Created = append(report.Created, file.RelPath)
			default:
				report.Updated = append(report.Updated, file.RelPath)
			}
		}
	}
	return report, nil
}

type fileOutcome struct {
	created bool
	updated bool
	skipped bool
}

func syncFile(ctx context.Context, client *Client, entry CorpusEntry, file FilePlan, opts Options) (fileOutcome, error) {
	var outcome fileOutcome

	sources, err := listSources(ctx, client)
	if err != nil {
		return outcome, fmt.Errorf("list sources: %w", err)
	}
	var source *apiSource
	for i := range sources {
		if sources[i].Title == file.RelPath && sources[i].DeletedAt == nil {
			source = &sources[i]
			break
		}
	}
	if source == nil {
		created, err := createSource(ctx, client, entry, file)
		if err != nil {
			return outcome, err
		}
		source = created
		outcome.created = true
	}

	versions, err := listVersions(ctx, client, source.ID)
	if err != nil {
		return outcome, fmt.Errorf("list versions: %w", err)
	}
	if len(versions) > 0 {
		latest := versions[len(versions)-1]
		if latest.ContentHash != nil && *latest.ContentHash == file.Hash && latest.Status == "PUBLISHED" {
			outcome.skipped = true
			return outcome, nil
		}
	}

	createdVersion, err := createVersion(ctx, client, source.ID, len(versions)+1, file)
	if err != nil {
		return outcome, err
	}
	if err := reviewVersion(ctx, client, source.ID, createdVersion.ID); err != nil {
		return outcome, err
	}
	jobID, err := publishVersion(ctx, client, source.ID, createdVersion.ID)
	if err != nil {
		return outcome, err
	}
	if err := waitJob(ctx, client, jobID, opts.JobTimeout); err != nil {
		return outcome, err
	}
	return outcome, nil
}

func listSources(ctx context.Context, c *Client) ([]apiSource, error) {
	var sources []apiSource
	if err := c.request(ctx, http.MethodGet, "/api/rag/sources", nil, &sources); err != nil {
		return nil, err
	}
	return sources, nil
}

func createSource(ctx context.Context, c *Client, entry CorpusEntry, file FilePlan) (*apiSource, error) {
	owner := entry.Owner
	description := entry.Description
	payload := map[string]any{
		"title":          file.RelPath,
		"description":    nullable(description),
		"source_type":    "document",
		"scope":          entry.Scope,
		"classification": entry.Classification,
		"language":       entry.Language,
		"tags":           append(append([]string{}, entry.Tags...), DocsAsCodeTag),
		"owner_id":       &owner,
	}
	var created apiSource
	if err := c.request(ctx, http.MethodPost, "/api/rag/sources", payload, &created); err != nil {
		return nil, fmt.Errorf("create source: %w", err)
	}
	if created.ID == 0 {
		return nil, fmt.Errorf("create source: service returned no id")
	}
	return &created, nil
}

func nullable(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func listVersions(ctx context.Context, c *Client, sourceID int64) ([]apiVersion, error) {
	var versions []apiVersion
	path := fmt.Sprintf("/api/rag/sources/%d/versions", sourceID)
	if err := c.request(ctx, http.MethodGet, path, nil, &versions); err != nil {
		return nil, err
	}
	sort.SliceStable(versions, func(i, j int) bool { return versions[i].ID < versions[j].ID })
	return versions, nil
}

func createVersion(ctx context.Context, c *Client, sourceID int64, versionNumber int, file FilePlan) (*apiVersion, error) {
	payload := map[string]any{
		"version":      fmt.Sprintf("docs-as-code-v%d", versionNumber),
		"content_type": "text/markdown",
		"content":      file.Content,
	}
	var created apiVersion
	path := fmt.Sprintf("/api/rag/sources/%d/versions", sourceID)
	if err := c.request(ctx, http.MethodPost, path, payload, &created); err != nil {
		return nil, fmt.Errorf("create version: %w", err)
	}
	if created.ID == 0 {
		return nil, fmt.Errorf("create version: service returned no id")
	}
	return &created, nil
}

func reviewVersion(ctx context.Context, c *Client, sourceID, versionID int64) error {
	path := fmt.Sprintf("/api/rag/sources/%d/versions/%d/review", sourceID, versionID)
	payload := map[string]any{
		"decision": "approve",
		"reason":   "docs-as-code auto-approval (manifest-managed corpus, hash-gated)",
	}
	return c.request(ctx, http.MethodPost, path, payload, nil)
}

func publishVersion(ctx context.Context, c *Client, sourceID, versionID int64) (string, error) {
	path := fmt.Sprintf("/api/rag/sources/%d/versions/%d/publish", sourceID, versionID)
	var result struct {
		JobID string `json:"job_id"`
	}
	if err := c.request(ctx, http.MethodPost, path, nil, &result); err != nil {
		return "", fmt.Errorf("publish: %w", err)
	}
	if strings.TrimSpace(result.JobID) == "" {
		return "", fmt.Errorf("publish: service returned no job id")
	}
	return result.JobID, nil
}

func waitJob(ctx context.Context, c *Client, jobID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	path := "/api/rag/jobs/" + url.PathEscape(jobID)
	for {
		var job apiJob
		if err := c.request(ctx, http.MethodGet, path, nil, &job); err != nil {
			return fmt.Errorf("job %s: %w", jobID, err)
		}
		switch job.Status {
		case "completed":
			return nil
		case "failed":
			return fmt.Errorf("job %s failed ingestion", jobID)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("job %s did not complete within %s", jobID, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
