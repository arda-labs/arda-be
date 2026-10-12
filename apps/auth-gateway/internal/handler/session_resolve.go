package handler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/auth-gateway/internal/iamclient"
	"github.com/arda-labs/arda/apps/auth-gateway/internal/session"
)

type sessionUserResolveCall struct {
	done   chan struct{}
	result sessionUserResolveResult
}

type sessionUserResolveResult struct {
	user    *session.UserInfo
	ok      bool
	revoked bool
}

func (h *BFFHandler) resolveSessionUser(ctx context.Context, fallback *session.UserInfo, useCache bool) (*session.UserInfo, bool, bool) {
	if fallback == nil {
		fallback = &session.UserInfo{}
	}
	if h.iamClient == nil {
		return nil, false, false
	}

	if fallback.UserID == "" && looksLikeUUID(fallback.Subject) {
		fallback.UserID = fallback.Subject
	}
	// A cache entry created before tenant memberships were introduced is not a
	// complete authorization context. Force one IAM refresh instead of serving
	// stale roles/organizations under the legacy implicit tenant.
	if useCache && h.cache != nil && sessionTenantContextComplete(fallback) {
		for _, key := range sessionUserCacheKeys(fallback.UserID, fallback.Subject, fallback.AuthVersion) {
			if uc, ok := h.cache.get(key); ok {
				if userContextRevoked(uc) {
					return nil, false, true
				}
				return sessionUserFromIAM(uc, fallback), true, false
			}
		}
	}

	return h.resolveSessionUserOnce(ctx, fallback)
}

func sessionUserComplete(user *session.UserInfo) bool {
	return user != nil &&
		strings.TrimSpace(user.UserID) != "" &&
		strings.TrimSpace(user.Subject) != "" &&
		user.AuthVersion > 0 &&
		user.GroupIDs != nil &&
		sessionTenantContextComplete(user)
}

func sessionTenantContextComplete(user *session.UserInfo) bool {
	if user == nil {
		return false
	}
	// The reserved system scope is intentionally outside the business tenant
	// registry and is used only by control-plane superadmins.
	if strings.EqualFold(strings.TrimSpace(user.TenantID), "system") {
		return user.GlobalCapabilitiesLoaded
	}
	return user.TenantMemberships != nil && user.GlobalCapabilitiesLoaded
}

// sessionAuthRefreshDue decides whether the BFF session user context must be
// re-resolved from IAM. High-risk routes always force a fresh check; otherwise
// the context is re-validated at most once per interval, so a revoked
// permission, role or disabled account stops working within that window
// instead of for the whole session TTL. A non-positive interval disables the
// scheduled check (only forceFresh re-validates).
func sessionAuthRefreshDue(now, lastCheck time.Time, interval time.Duration, forceFresh bool) bool {
	if forceFresh {
		return true
	}
	if interval <= 0 {
		return false
	}
	if lastCheck.IsZero() {
		return true
	}
	return now.Sub(lastCheck) >= interval
}

func (h *BFFHandler) sessionAuthCheckInterval() time.Duration {
	if h.cfg.SessionAuthCheckInterval <= 0 {
		return 0
	}
	return time.Duration(h.cfg.SessionAuthCheckInterval) * time.Second
}

// userContextRevoked reports whether IAM says the account may no longer hold a
// session. Disabling a user bumps auth_version but leaves roles/permissions
// untouched, so the status field is what actually stops a disabled account.
func userContextRevoked(uc *iamclient.UserContext) bool {
	return uc != nil && strings.EqualFold(strings.TrimSpace(uc.Status), "DISABLED")
}

func (h *BFFHandler) ensureSessionUser(ctx context.Context, sess *session.Session, forceFresh bool) bool {
	if sess == nil {
		return false
	}
	due := sessionAuthRefreshDue(time.Now(), sess.LastAuthCheck, h.sessionAuthCheckInterval(), forceFresh)
	if !due && sessionUserComplete(sess.User) {
		return true
	}
	previous := sess.User
	// Scheduled re-validation bypasses the local context cache so it observes
	// the current IAM state; the legacy "incomplete context" path keeps using
	// the cache like before.
	userInfo, ok, revoked := h.resolveSessionUser(ctx, sess.User, !due)
	if revoked {
		h.deleteSession(ctx, sess.ID)
		return false
	}
	if !ok || userInfo == nil || userInfo.UserID == "" {
		// IAM is temporarily unreachable: keep serving the last validated
		// context instead of logging every live session out during an outage.
		// LastAuthCheck stays untouched, so the next request retries.
		if due && !forceFresh && sessionUserComplete(previous) {
			return true
		}
		return false
	}
	sess.User = userInfo
	if due {
		sess.LastAuthCheck = time.Now()
	}
	if !reflect.DeepEqual(previous, userInfo) || due {
		h.updateSession(ctx, sess)
	}
	return true
}

func (h *BFFHandler) deleteSession(ctx context.Context, sessionID string) {
	if sessionID == "" {
		return
	}
	if err := h.store.Delete(ctx, sessionID); err != nil {
		h.logger.Warn("delete revoked session failed", "session_id", sessionID, "err", err)
		return
	}
	h.logger.Info("session revoked", "session_id", sessionID)
}

func (h *BFFHandler) resolveSessionUserOnce(ctx context.Context, fallback *session.UserInfo) (*session.UserInfo, bool, bool) {
	key := sessionUserResolveKey(fallback)
	if key == "" {
		return h.resolveSessionUserUncached(ctx, fallback)
	}

	h.resolveMu.Lock()
	if call, ok := h.resolveInflight[key]; ok {
		h.resolveMu.Unlock()
		<-call.done
		result := call.result
		return result.user, result.ok, result.revoked
	}
	call := &sessionUserResolveCall{done: make(chan struct{})}
	h.resolveInflight[key] = call
	h.resolveMu.Unlock()

	user, ok, revoked := h.resolveSessionUserUncached(ctx, fallback)
	call.result = sessionUserResolveResult{user: user, ok: ok, revoked: revoked}

	h.resolveMu.Lock()
	delete(h.resolveInflight, key)
	h.resolveMu.Unlock()
	close(call.done)
	return user, ok, revoked
}

func (h *BFFHandler) resolveSessionUserUncached(ctx context.Context, fallback *session.UserInfo) (*session.UserInfo, bool, bool) {
	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	// IAM answers 404 when the account is gone; a transport or server failure
	// must never be mistaken for it. Only when every attempted lookup reports
	// "not found" is the session revoked.
	attempted := 0
	allNotFound := true
	recordFailure := func(err error) {
		if !errors.Is(err, iamclient.ErrUserNotFound) {
			allNotFound = false
		}
	}

	if fallback.Subject != "" && !looksLikeUUID(fallback.Subject) {
		attempted++
		if uc, err := h.iamClient.GetUserBySubject(lookupCtx, fallback.Subject); err == nil {
			if userContextRevoked(uc) {
				return fallback, false, true
			}
			h.cacheSessionUser(fallback, uc)
			return sessionUserFromIAM(uc, fallback), true, false
		} else {
			recordFailure(err)
			h.logger.Warn("resolve user by subject failed", "subject", fallback.Subject, "err", err)
		}
	}

	for _, id := range iamLookupIDs(fallback) {
		attempted++
		if uc, err := h.iamClient.GetUserByID(lookupCtx, id); err == nil {
			if userContextRevoked(uc) {
				return fallback, false, true
			}
			h.cacheSessionUser(fallback, uc)
			return sessionUserFromIAM(uc, fallback), true, false
		} else {
			recordFailure(err)
			h.logger.Warn("resolve user by id failed", "user_id", id, "err", err)
		}
	}

	return fallback, false, attempted > 0 && allNotFound
}

func sessionUserResolveKey(info *session.UserInfo) string {
	if info == nil {
		return ""
	}
	if info.AuthVersion > 0 {
		if keys := sessionUserCacheKeys(info.UserID, info.Subject, info.AuthVersion); len(keys) > 0 {
			return keys[0]
		}
	}
	for _, base := range []string{info.UserID, info.Subject} {
		base = strings.TrimSpace(base)
		if base != "" {
			return base
		}
	}
	return ""
}

func (h *BFFHandler) cacheSessionUser(fallback *session.UserInfo, uc *iamclient.UserContext) {
	if h.cache == nil || uc == nil {
		return
	}
	for _, key := range sessionUserCacheKeys(uc.UserID, uc.Subject, uc.AuthVersion) {
		h.cache.set(key, uc)
	}
	if fallback != nil && (fallback.AuthVersion <= 0 || fallback.AuthVersion == uc.AuthVersion) {
		for _, key := range sessionUserCacheKeys(fallback.UserID, fallback.Subject, fallback.AuthVersion) {
			h.cache.set(key, uc)
		}
	}
}

func sessionUserCacheKeys(userID, subject string, authVersion int64) []string {
	version := fmt.Sprintf("v%d", authVersion)
	if authVersion <= 0 {
		version = "legacy"
	}
	seen := map[string]struct{}{}
	var keys []string
	for _, base := range []string{strings.TrimSpace(userID), strings.TrimSpace(subject)} {
		if base == "" {
			continue
		}
		key := fmt.Sprintf("%s:%s", base, version)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	return keys
}

func iamLookupIDs(info *session.UserInfo) []string {
	if info == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var ids []string
	for _, id := range []string{info.UserID, info.Subject} {
		id = strings.TrimSpace(id)
		if id == "" || !looksLikeUUID(id) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func sessionUserFromIAM(uc *iamclient.UserContext, fallback *session.UserInfo) *session.UserInfo {
	if uc == nil {
		return fallback
	}
	info := &session.UserInfo{
		UserID:                   uc.UserID,
		Subject:                  uc.Subject,
		Username:                 uc.Username,
		Email:                    uc.Email,
		DisplayName:              uc.DisplayName,
		Nickname:                 uc.Nickname,
		FirstName:                uc.FirstName,
		LastName:                 uc.LastName,
		PhoneNumber:              uc.PhoneNumber,
		Birthdate:                uc.Birthdate,
		Gender:                   uc.Gender,
		Address:                  uc.Address,
		Country:                  uc.Country,
		Picture:                  uc.PictureURL,
		AvatarFileID:             uc.AvatarFileID,
		CoverImage:               uc.CoverImageURL,
		CoverFileID:              uc.CoverFileID,
		TenantID:                 uc.TenantID,
		ActiveTenantID:           uc.ActiveTenantID,
		TenantSelectionRequired:  uc.TenantSelectionRequired,
		OrgIDs:                   uc.OrgIDs,
		GroupIDs:                 append([]string(nil), uc.GroupIDs...),
		Roles:                    uc.Roles,
		Permissions:              uc.Permissions,
		GlobalRoles:              uc.GlobalRoles,
		GlobalPermissions:        uc.GlobalPermissions,
		IsGlobalAdmin:            uc.IsGlobalAdmin,
		GlobalCapabilitiesLoaded: uc.GlobalCapabilitiesLoaded,
		AuthVersion:              uc.AuthVersion,
		Timezone:                 uc.Timezone,
		Locale:                   uc.Locale,
	}
	if len(uc.TenantMemberships) > 0 {
		info.TenantMemberships = make([]session.TenantMembership, 0, len(uc.TenantMemberships))
		for _, membership := range uc.TenantMemberships {
			info.TenantMemberships = append(info.TenantMemberships, session.TenantMembership{
				TenantID: membership.TenantID, TenantCode: membership.TenantCode,
				TenantName: membership.TenantName, TenantStatus: membership.TenantStatus,
				Status: membership.Status, IsDefault: membership.IsDefault,
			})
		}
	}
	if info.Subject == "" && fallback != nil {
		info.Subject = fallback.Subject
	}
	if info.Username == "" && fallback != nil {
		info.Username = fallback.Username
	}
	if info.Email == "" && fallback != nil {
		info.Email = fallback.Email
	}
	if info.DisplayName == "" && fallback != nil {
		info.DisplayName = fallback.DisplayName
	}
	if info.Nickname == "" && fallback != nil {
		info.Nickname = fallback.Nickname
	}
	if info.FirstName == "" && fallback != nil {
		info.FirstName = fallback.FirstName
	}
	if info.LastName == "" && fallback != nil {
		info.LastName = fallback.LastName
	}
	if info.PhoneNumber == "" && fallback != nil {
		info.PhoneNumber = fallback.PhoneNumber
	}
	if info.Birthdate == "" && fallback != nil {
		info.Birthdate = fallback.Birthdate
	}
	if info.Gender == "" && fallback != nil {
		info.Gender = fallback.Gender
	}
	if info.Address == "" && fallback != nil {
		info.Address = fallback.Address
	}
	if info.Country == "" && fallback != nil {
		info.Country = fallback.Country
	}
	if info.CoverImage == "" && fallback != nil {
		info.CoverImage = fallback.CoverImage
	}
	if info.CoverFileID == "" && fallback != nil {
		info.CoverFileID = fallback.CoverFileID
	}
	return info
}
