package ardadoc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// MediaUploader stores artifacts into media-service over its direct multipart
// upload endpoint. Tenant context is copied from the verified inbound request
// headers the domain service received (media-service trusts the gateway-set
// X-Tenant-Id / X-Org-Id / X-User-Id headers), mirroring how
// libs/go/arda-media propagates identity over gRPC.
type MediaUploader struct {
	baseURL    string
	httpClient *http.Client
}

// Gateway identity headers media-service reads on upload.
const (
	headerTenantID = "X-Tenant-Id"
	headerOrgID    = "X-Org-Id"
	headerUserID   = "X-User-Id"
	headerActorID  = "X-Actor-User-Id"
)

// NewMediaUploader creates an uploader for the media-service base URL
// (e.g. http://media-service.arda-app.svc.cluster.local:8080).
func NewMediaUploader(baseURL string, opts ...ClientOption) (*MediaUploader, error) {
	base, err := normalizeBaseURL(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid media base url: %w", err)
	}
	return &MediaUploader{baseURL: base, httpClient: newClientConfig(opts).httpClient}, nil
}

// Upload stores content as a new media file and returns its public_id. The
// file is created in status 'temp' with an expiry; callers that need it
// permanently linked to an entity must call Attach (SnapshotPipeline does
// this automatically when EntityType/EntityID are set).
func (u *MediaUploader) Upload(ctx context.Context, srcHeader http.Header, filename, contentType, module string, content []byte) (string, error) {
	filename = strings.TrimSpace(filename)
	module = strings.TrimSpace(module)
	if filename == "" {
		return "", fmt.Errorf("filename is required")
	}
	if module == "" {
		return "", fmt.Errorf("module is required")
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := writeFormFile(mw, filename, content, contentType); err != nil {
		return "", fmt.Errorf("write media form: %w", err)
	}
	if err := mw.WriteField("module", module); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(u.baseURL, "/")+"/api/media", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	copyScopeHeaders(req.Header, srcHeader)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("media upload: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read media upload response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("media upload status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var envelope struct {
		Result struct {
			PublicID string `json:"public_id"`
		} `json:"result"`
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("decode media upload response: %w", err)
	}
	if !envelope.Success || envelope.Result.PublicID == "" {
		return "", fmt.Errorf("media upload returned no public_id")
	}
	return envelope.Result.PublicID, nil
}

// Attach moves uploaded temp files into status 'attached' linked to the given
// owner entity (entity_type + entity_id) through the media-service attach
// endpoint — the same transition the FE performs, so files survive temp
// cleanup and show up in entity listings.
func (u *MediaUploader) Attach(ctx context.Context, srcHeader http.Header, publicIDs []string, ownerType, ownerID string) error {
	if len(publicIDs) == 0 || strings.TrimSpace(ownerType) == "" || strings.TrimSpace(ownerID) == "" {
		return fmt.Errorf("public_ids, owner_type and owner_id are required")
	}
	payload, err := json.Marshal(map[string]any{
		"public_ids": publicIDs,
		"owner_type": ownerType,
		"owner_id":   ownerID,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(u.baseURL, "/")+"/api/media/files/attach", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	copyScopeHeaders(req.Header, srcHeader)

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("media attach: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read media attach response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("media attach status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

// copyScopeHeaders forwards tenant/organization/user identity headers from
// the inbound request to the media-service call.
func copyScopeHeaders(dst, src http.Header) {
	if src == nil {
		return
	}
	for _, name := range []string{headerTenantID, headerOrgID, headerUserID, headerActorID} {
		if value := strings.TrimSpace(src.Get(name)); value != "" {
			dst.Set(name, value)
		}
	}
}
