package service

import (
	"context"
	"fmt"

	"github.com/arda-labs/arda/apps/iam-service/internal/domain"
	"github.com/arda-labs/arda/apps/iam-service/internal/repository"
)

// UserService orchestrates user-related business logic.
type UserService struct {
	repo     *repository.UserRepository
	identity *IdentityService
	tenant   *repository.TenantRepository
}

// NewUserService creates a new user service.
func NewUserService(repo *repository.UserRepository, identity *IdentityService, tenant ...*repository.TenantRepository) *UserService {
	var tenantRepo *repository.TenantRepository
	if len(tenant) > 0 {
		tenantRepo = tenant[0]
	}
	return &UserService{repo: repo, identity: identity, tenant: tenantRepo}
}

// GetUserContextBySubject builds a user context from an external subject.
func (s *UserService) GetUserContextBySubject(ctx context.Context, subject string) (*domain.UserContext, error) {
	user, err := s.repo.GetUserBySubject(ctx, subject)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

// GetUserContextByID builds a user context from a user UUID.
func (s *UserService) GetUserContextByID(ctx context.Context, id string) (*domain.UserContext, error) {
	user, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

// GetUserContextByIDForTenant returns context for an explicitly selected
// active membership. The tenant ID is never accepted without an IAM lookup.
func (s *UserService) GetUserContextByIDForTenant(ctx context.Context, id, tenantID string) (*domain.UserContext, error) {
	user, err := s.repo.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	if s.tenant == nil {
		return nil, fmt.Errorf("tenant repository is not configured")
	}
	membership, err := s.tenant.GetForUser(ctx, user.ID, tenantID)
	if err != nil {
		return nil, err
	}
	if membership == nil {
		return nil, fmt.Errorf("user is not an active member of the tenant")
	}
	return s.buildContextForTenant(ctx, user, tenantID)
}

func (s *UserService) GetUserContextByKratosIdentityID(ctx context.Context, identityID string) (*domain.UserContext, error) {
	user, err := s.repo.GetUserByKratosIdentityID(ctx, identityID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

func (s *UserService) ResolveOrLinkKratosIdentity(ctx context.Context, identityID, email, name string) (*domain.UserContext, error) {
	user, err := s.repo.GetUserByKratosIdentityID(ctx, identityID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return s.buildContext(ctx, user)
	}
	if email == "" {
		return nil, fmt.Errorf("user not found")
	}
	user, err = s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	if user.DisplayName == "" {
		user.DisplayName = name
	}
	if err := s.identity.LinkIdentity(ctx, user, identityID); err != nil {
		return nil, err
	}
	linkedUser, err := s.repo.GetUserByID(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return s.buildContext(ctx, linkedUser)
}

func (s *UserService) ResolveOrLinkIdentity(ctx context.Context, providerID, externalID, email, name string, emailVerified bool) (*domain.UserContext, error) {
	if providerID == "" || externalID == "" {
		return nil, fmt.Errorf("provider id and external id are required")
	}
	mapping, err := s.repo.FindIdentityMapping(ctx, providerID, externalID)
	if err != nil {
		return nil, err
	}
	if mapping != nil {
		if !mapping.IsActive {
			return nil, fmt.Errorf("identity mapping is inactive")
		}
		return s.GetUserContextByID(ctx, mapping.InternalUserID)
	}
	if email == "" {
		return nil, fmt.Errorf("user not found")
	}
	if !emailVerified {
		return nil, fmt.Errorf("verified email is required for first-time identity link")
	}
	user, err := s.repo.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	if user.DisplayName == "" {
		user.DisplayName = name
	}
	if err := s.repo.CreateIdentityMapping(ctx, &domain.IdentityMapping{
		ProviderID:     providerID,
		ExternalID:     externalID,
		InternalUserID: user.ID,
		IsActive:       true,
	}); err != nil {
		return nil, err
	}
	return s.buildContext(ctx, user)
}

func (s *UserService) UpdateUserAvatar(ctx context.Context, userID, avatarFileID, pictureURL string) (*domain.UserContext, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	if avatarFileID == "" && pictureURL == "" {
		return nil, fmt.Errorf("avatar_file_id or picture_url is required")
	}
	user, err := s.repo.UpdateUserAvatar(ctx, userID, avatarFileID, pictureURL)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

func (s *UserService) UpdateUserCover(ctx context.Context, userID, coverFileID, coverImageURL string) (*domain.UserContext, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	if coverFileID == "" && coverImageURL == "" {
		return nil, fmt.Errorf("cover_file_id or cover_image_url is required")
	}
	user, err := s.repo.UpdateUserCover(ctx, userID, coverFileID, coverImageURL)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

func (s *UserService) UpdateUserProfile(ctx context.Context, userID, name, nickname, firstName, lastName, phoneNumber, birthdate, gender, address, country, position, department, employeeID, approvalLevel, dailyLimit, bio string) (*domain.UserContext, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	user, err := s.repo.UpdateUserProfile(ctx, userID, name, nickname, firstName, lastName, phoneNumber, birthdate, gender, address, country, position, department, employeeID, approvalLevel, dailyLimit, bio)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}
	return s.buildContext(ctx, user)
}

func (s *UserService) buildContext(ctx context.Context, user *domain.User) (*domain.UserContext, error) {
	activeTenantID := user.TenantID
	var memberships []domain.TenantMembership
	if s.tenant != nil {
		var err error
		memberships, err = s.tenant.ListForUser(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		for _, membership := range memberships {
			if membership.IsDefault {
				activeTenantID = membership.TenantID
				break
			}
		}
		if activeTenantID == "" && len(memberships) == 1 {
			activeTenantID = memberships[0].TenantID
		}
	}
	return s.buildContextForTenantWithMemberships(ctx, user, activeTenantID, memberships)
}

func (s *UserService) buildContextForTenant(ctx context.Context, user *domain.User, tenantID string) (*domain.UserContext, error) {
	memberships, err := s.tenant.ListForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return s.buildContextForTenantWithMemberships(ctx, user, tenantID, memberships)
}

func (s *UserService) buildContextForTenantWithMemberships(ctx context.Context, user *domain.User, activeTenantID string, memberships []domain.TenantMembership) (*domain.UserContext, error) {
	roles, err := s.repo.GetUserRoles(ctx, user.ID)
	globalRoles := []domain.Role(nil)
	if activeTenantID != "" && s.tenant != nil {
		roles, err = s.repo.GetUserRolesForTenant(ctx, user.ID, activeTenantID)
	}
	if err != nil {
		return nil, err
	}
	if s.tenant != nil {
		globalRoles, err = s.repo.GetUserRolesForSystem(ctx, user.ID)
		if err != nil {
			return nil, err
		}
	}

	perms, err := s.repo.GetUserPermissions(ctx, user.ID)
	globalPerms := []domain.Permission(nil)
	if activeTenantID != "" && s.tenant != nil {
		perms, err = s.repo.GetUserPermissionsForTenant(ctx, user.ID, activeTenantID)
	}
	if err != nil {
		return nil, err
	}
	if s.tenant != nil {
		globalPerms, err = s.repo.GetUserPermissionsForSystem(ctx, user.ID)
		if err != nil {
			return nil, err
		}
	}

	orgs, err := s.repo.GetUserOrganizations(ctx, user.ID)
	if activeTenantID != "" && s.tenant != nil {
		orgs, err = s.repo.GetUserOrganizationsForTenant(ctx, user.ID, activeTenantID)
	}
	if err != nil {
		return nil, err
	}

	groupIDs, err := s.repo.GetUserGroupIDs(ctx, user.ID)
	if activeTenantID != "" && s.tenant != nil {
		groupIDs, err = s.repo.GetUserGroupIDsForTenant(ctx, user.ID, activeTenantID)
	}
	if err != nil {
		return nil, err
	}
	if groupIDs == nil {
		groupIDs = []string{}
	}

	authVersion, err := s.repo.GetAuthVersion(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	roleCodes := make([]string, len(roles))
	for i, r := range roles {
		roleCodes[i] = r.Code
	}

	permCodes := make([]string, len(perms))
	for i, p := range perms {
		permCodes[i] = p.Code
	}
	globalRoleCodes := make([]string, len(globalRoles))
	for i, r := range globalRoles {
		globalRoleCodes[i] = r.Code
	}
	globalPermCodes := make([]string, len(globalPerms))
	for i, p := range globalPerms {
		globalPermCodes[i] = p.Code
	}
	isGlobalAdmin := false
	for _, code := range globalRoleCodes {
		if code == "SUPER_ADMIN" {
			isGlobalAdmin = true
			break
		}
	}
	if !isGlobalAdmin {
		for _, code := range globalPermCodes {
			if code == "superadmin" {
				isGlobalAdmin = true
				break
			}
		}
	}

	return &domain.UserContext{
		UserID:                   user.ID,
		Subject:                  user.Subject,
		Username:                 user.Username,
		Email:                    user.Email,
		DisplayName:              user.DisplayName,
		Nickname:                 user.Nickname,
		FirstName:                user.FirstName,
		LastName:                 user.LastName,
		PhoneNumber:              user.PhoneNumber,
		Birthdate:                user.Birthdate,
		Gender:                   user.Gender,
		Address:                  user.Address,
		Country:                  user.Country,
		PictureURL:               user.PictureURL,
		AvatarFileID:             user.AvatarFileID,
		CoverImageURL:            user.CoverImageURL,
		CoverFileID:              user.CoverFileID,
		TenantID:                 activeTenantID,
		ActiveTenantID:           activeTenantID,
		TenantMemberships:        memberships,
		TenantSelectionRequired:  len(memberships) > 1 && activeTenantID == "",
		OrgIDs:                   orgs,
		GroupIDs:                 groupIDs,
		Roles:                    roleCodes,
		Permissions:              permCodes,
		GlobalRoles:              globalRoleCodes,
		GlobalPermissions:        globalPermCodes,
		IsGlobalAdmin:            isGlobalAdmin,
		GlobalCapabilitiesLoaded: s.tenant != nil,
		AuthVersion:              authVersion,
		Department:               user.Department,
		Position:                 user.Position,
		EmployeeID:               user.EmployeeID,
		ApprovalLevel:            user.ApprovalLevel,
		DailyLimit:               user.DailyLimit,
		Bio:                      user.Bio,
	}, nil
}

func (s *UserService) UpdateUserEmail(ctx context.Context, userID, newEmail string) (*domain.UserContext, error) {
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}
	if newEmail == "" {
		return nil, fmt.Errorf("email is required")
	}

	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	if user.Email == newEmail {
		return s.buildContext(ctx, user)
	}

	if s.identity.CanManageIdentity(ctx, user) {
		updatedUser, err := s.identity.UpdateEmail(ctx, user, newEmail)
		if err != nil {
			return nil, fmt.Errorf("failed to update Kratos identity: %w", err)
		}
		return s.buildContext(ctx, updatedUser)
	}

	updatedUser, err := s.repo.UpdateUserEmail(ctx, userID, newEmail)
	if err != nil {
		return nil, fmt.Errorf("failed to update user email in DB: %w", err)
	}
	if updatedUser == nil {
		return nil, fmt.Errorf("user not found after update")
	}

	return s.buildContext(ctx, updatedUser)
}

func (s *UserService) UpdateUserPassword(ctx context.Context, userID, newPassword string) error {
	if userID == "" {
		return fmt.Errorf("user id is required")
	}
	user, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to fetch user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("user not found")
	}
	return s.identity.UpdatePassword(ctx, user, newPassword)
}
