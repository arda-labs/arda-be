package grpc

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	ardaBusinessDate "github.com/arda-labs/arda/libs/go/arda-businessdate"
	"github.com/arda-labs/arda/libs/go/arda-grpc/interceptors"
	ardametadata "github.com/arda-labs/arda/libs/go/arda-grpc/metadata"
	platformv1 "github.com/arda-labs/arda/libs/go/arda-proto/platform/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type PlatformServer struct {
	platformv1.UnimplementedPlatformServiceServer
	svc      *service.PlatformService
	calendar *service.CalendarService
}

func NewPlatformServer(svc *service.PlatformService, calendars ...*service.CalendarService) *PlatformServer {
	server := &PlatformServer{svc: svc}
	if len(calendars) > 0 {
		server.calendar = calendars[0]
	}
	return server
}

func (s *PlatformServer) GetBusinessDate(ctx context.Context, req *platformv1.GetBusinessDateRequest) (*platformv1.BusinessDate, error) {
	if s.calendar == nil {
		return nil, status.Error(codes.Unavailable, "business-date calendar is unavailable")
	}
	scope := req.GetScope()
	tenantID, err := verifiedTenant(ctx, scope.GetTenantId())
	if err != nil {
		return nil, err
	}
	resolved, err := businessDateScope(tenantID, scope)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	item, err := s.calendar.BusinessDateForScope(ctx, resolved)
	if err != nil {
		if errors.Is(err, domain.ErrSystemDateNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if errors.Is(err, domain.ErrBusinessDateScopeMappingRequired) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &platformv1.BusinessDate{
		PreviousBusinessDate: item.PreviousBusinessDate.Format("2006-01-02"),
		BusinessDate:         item.CurrentBusinessDate.Format("2006-01-02"),
		NextBusinessDate:     item.NextBusinessDate.Format("2006-01-02"),
		Status:               item.Status,
	}, nil
}

func (s *PlatformServer) IsWorkingDay(ctx context.Context, req *platformv1.IsWorkingDayRequest) (*platformv1.IsWorkingDayResponse, error) {
	if s.calendar == nil {
		return nil, status.Error(codes.Unavailable, "business-date calendar is unavailable")
	}
	scope := req.GetScope()
	tenantID, err := verifiedTenant(ctx, scope.GetTenantId())
	if err != nil {
		return nil, err
	}
	resolved, err := businessDateScope(tenantID, scope)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	date, err := time.Parse("2006-01-02", req.GetDate())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "date must use YYYY-MM-DD")
	}
	working, err := ardaBusinessDate.IsWorkingDay(ctx, s.calendar, resolved, date)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessDateScopeMappingRequired) {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &platformv1.IsWorkingDayResponse{IsWorkingDay: working}, nil
}

func businessDateScope(tenantID string, scope *platformv1.ScopeSelector) (ardaBusinessDate.Scope, error) {
	result := ardaBusinessDate.Scope{TenantID: tenantID, Type: ardaBusinessDate.ScopeType(strings.ToUpper(strings.TrimSpace(scope.GetScopeType()))), OrgCode: strings.TrimSpace(scope.GetScopeId())}
	if err := ardaBusinessDate.ValidateScope(result); err != nil {
		return ardaBusinessDate.Scope{}, err
	}
	return result, nil
}

func verifiedTenant(ctx context.Context, requested string) (string, error) {
	tenantID := strings.TrimSpace(ardametadata.FromIncoming(ctx).TenantID)
	if tenantID == "" {
		return "", status.Error(codes.InvalidArgument, "verified tenant scope is required")
	}
	if requested != "" && strings.TrimSpace(requested) != tenantID {
		return "", status.Error(codes.PermissionDenied, "requested tenant is outside verified scope")
	}
	return tenantID, nil
}

func (s *PlatformServer) ListParameters(ctx context.Context, req *platformv1.ListParametersRequest) (*platformv1.ListParametersResponse, error) {
	scope := req.GetScope()
	tenantID, err := verifiedTenant(ctx, scope.GetTenantId())
	if err != nil {
		return nil, err
	}
	items, err := s.svc.ListParameters(ctx, tenantID, scope.GetScopeType(), scope.GetScopeId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &platformv1.ListParametersResponse{Parameters: make([]*platformv1.Parameter, 0, len(items))}
	for _, item := range items {
		resp.Parameters = append(resp.Parameters, parameterToProto(item))
	}
	return resp, nil
}

func (s *PlatformServer) UpsertParameter(ctx context.Context, req *platformv1.UpsertParameterRequest) (*platformv1.Parameter, error) {
	if req.GetParameter() == nil {
		return nil, status.Error(codes.InvalidArgument, "parameter is required")
	}
	tenantID, err := verifiedTenant(ctx, req.GetParameter().GetTenantId())
	if err != nil {
		return nil, err
	}
	item, err := parameterFromProto(req.GetParameter())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	item.TenantID = &tenantID
	item, err = s.svc.UpsertParameter(ctx, item)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return parameterToProto(item), nil
}

func (s *PlatformServer) ResolveParameter(ctx context.Context, req *platformv1.ResolveParameterRequest) (*platformv1.Parameter, error) {
	if req.GetModule() == "" || req.GetKey() == "" || req.GetEffectiveDate() == "" {
		return nil, status.Error(codes.InvalidArgument, "module, key, and effective_date are required")
	}
	effectiveDate, err := time.Parse("2006-01-02", req.GetEffectiveDate())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "effective_date must use YYYY-MM-DD")
	}
	tenantID := strings.TrimSpace(ardametadata.FromIncoming(ctx).TenantID)
	if tenantID == "" {
		claims, authenticated := interceptors.ServiceClaims(ctx)
		if !authenticated || claims.Source != "loan-service" || strings.TrimSpace(req.GetTenantId()) != "" {
			return nil, status.Error(codes.InvalidArgument, "verified tenant scope is required")
		}
		for _, scope := range req.GetScopes() {
			if strings.ToLower(scope.GetScopeType()) != domain.ScopeGlobal || scope.GetTenantId() != "" || scope.GetScopeId() != "" {
				return nil, status.Error(codes.PermissionDenied, "loan-service may resolve only global parameters without a tenant")
			}
		}
	} else if req.GetTenantId() != "" && strings.TrimSpace(req.GetTenantId()) != tenantID {
		return nil, status.Error(codes.PermissionDenied, "requested tenant is outside verified scope")
	}
	scopes := make([]service.ScopeSelector, 0, len(req.GetScopes()))
	for _, scope := range req.GetScopes() {
		if scope.GetTenantId() != "" && strings.TrimSpace(scope.GetTenantId()) != tenantID {
			return nil, status.Error(codes.PermissionDenied, "requested tenant is outside verified scope")
		}
		scopes = append(scopes, service.ScopeSelector{
			TenantID:  tenantID,
			ScopeType: strings.ToLower(scope.GetScopeType()),
			ScopeID:   scope.GetScopeId(),
		})
	}
	item, err := s.svc.ResolveParameter(ctx, tenantID, req.GetModule(), req.GetKey(), scopes, effectiveDate)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, status.Error(codes.NotFound, "parameter not found for requested scope and effective date")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return parameterToProto(item), nil
}

func (s *PlatformServer) ListLookupCategories(ctx context.Context, req *platformv1.ListLookupCategoriesRequest) (*platformv1.ListLookupCategoriesResponse, error) {
	scope := req.GetScope()
	tenantID, err := verifiedTenant(ctx, scope.GetTenantId())
	if err != nil {
		return nil, err
	}
	items, err := s.svc.ListLookupCategories(ctx, tenantID, scope.GetScopeType(), scope.GetScopeId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &platformv1.ListLookupCategoriesResponse{Categories: make([]*platformv1.LookupCategory, 0, len(items))}
	for _, item := range items {
		resp.Categories = append(resp.Categories, lookupCategoryToProto(item))
	}
	return resp, nil
}

func (s *PlatformServer) UpsertLookupCategory(ctx context.Context, req *platformv1.UpsertLookupCategoryRequest) (*platformv1.LookupCategory, error) {
	if req.GetCategory() == nil {
		return nil, status.Error(codes.InvalidArgument, "category is required")
	}
	tenantID, err := verifiedTenant(ctx, req.GetCategory().GetTenantId())
	if err != nil {
		return nil, err
	}
	item := lookupCategoryFromProto(req.GetCategory())
	item.TenantID = &tenantID
	item, err = s.svc.UpsertLookupCategory(ctx, item)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return lookupCategoryToProto(item), nil
}

func (s *PlatformServer) ListLookupValues(ctx context.Context, req *platformv1.ListLookupValuesRequest) (*platformv1.ListLookupValuesResponse, error) {
	if req.GetCategoryCode() == "" {
		return nil, status.Error(codes.InvalidArgument, "category_code is required")
	}
	tenantID, err := verifiedTenant(ctx, "")
	if err != nil {
		return nil, err
	}
	items, err := s.svc.ListLookupValues(ctx, tenantID, req.GetCategoryCode())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &platformv1.ListLookupValuesResponse{Values: make([]*platformv1.LookupValue, 0, len(items))}
	for _, item := range items {
		resp.Values = append(resp.Values, lookupValueToProto(item))
	}
	return resp, nil
}

func (s *PlatformServer) UpsertLookupValue(ctx context.Context, req *platformv1.UpsertLookupValueRequest) (*platformv1.LookupValue, error) {
	if req.GetCategoryCode() == "" || req.GetValue() == nil {
		return nil, status.Error(codes.InvalidArgument, "category_code and value are required")
	}
	tenantID, err := verifiedTenant(ctx, "")
	if err != nil {
		return nil, err
	}
	item, err := s.svc.UpsertLookupValue(ctx, tenantID, req.GetCategoryCode(), lookupValueFromProto(req.GetValue()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return lookupValueToProto(item), nil
}

func (s *PlatformServer) ListOrganizations(ctx context.Context, req *platformv1.ListOrganizationsRequest) (*platformv1.ListOrganizationsResponse, error) {
	tenantID, err := verifiedTenant(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	items, total, err := s.svc.ListOrganizations(ctx, repository.ListOrganizationsParams{
		TenantID: tenantID,
		Unpaged:  true,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	_ = total
	resp := &platformv1.ListOrganizationsResponse{Organizations: make([]*platformv1.Organization, 0, len(items))}
	for _, item := range items {
		resp.Organizations = append(resp.Organizations, organizationToProto(item))
	}
	return resp, nil
}

func (s *PlatformServer) CreateOrganization(ctx context.Context, req *platformv1.CreateOrganizationRequest) (*platformv1.Organization, error) {
	if req.GetOrganization() == nil {
		return nil, status.Error(codes.InvalidArgument, "organization is required")
	}
	tenantID, err := verifiedTenant(ctx, req.GetOrganization().GetTenantId())
	if err != nil {
		return nil, err
	}
	item := organizationFromProto(req.GetOrganization())
	item.TenantID = tenantID
	item, err = s.svc.CreateOrganization(ctx, item)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return organizationToProto(item), nil
}

func (s *PlatformServer) ListAdminUnits(ctx context.Context, req *platformv1.ListAdminUnitsRequest) (*platformv1.ListAdminUnitsResponse, error) {
	items, err := s.svc.ListGeoAdminUnits(ctx, req.GetParentCode(), int(req.GetLevel()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	resp := &platformv1.ListAdminUnitsResponse{AdminUnits: make([]*platformv1.AdminUnit, 0, len(items))}
	for _, item := range items {
		resp.AdminUnits = append(resp.AdminUnits, adminUnitToProto(item))
	}
	return resp, nil
}

func (s *PlatformServer) UpsertAdminUnit(ctx context.Context, req *platformv1.UpsertAdminUnitRequest) (*platformv1.AdminUnit, error) {
	if req.GetAdminUnit() == nil {
		return nil, status.Error(codes.InvalidArgument, "admin_unit is required")
	}
	item, err := s.svc.UpsertGeoAdminUnit(ctx, adminUnitFromProto(req.GetAdminUnit()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return adminUnitToProto(item), nil
}

func parameterToProto(item domain.Parameter) *platformv1.Parameter {
	return &platformv1.Parameter{
		Id:            item.ID,
		TenantId:      deref(item.TenantID),
		Module:        item.Module,
		Key:           item.Key,
		Value:         item.Value,
		ValueType:     item.ValueType,
		Unit:          item.Unit,
		ScopeType:     item.ScopeType,
		ScopeId:       deref(item.ScopeID),
		EffectiveFrom: item.EffectiveFrom.Format("2006-01-02"),
		EffectiveTo:   formatDate(item.EffectiveTo),
		Description:   deref(item.Description),
		IsSecret:      item.IsSecret,
		CreatedAt:     timestamppb.New(item.CreatedAt),
		UpdatedAt:     timestamppb.New(item.UpdatedAt),
	}
}

func parameterFromProto(item *platformv1.Parameter) (domain.Parameter, error) {
	var from time.Time
	if item.GetEffectiveFrom() != "" {
		var err error
		from, err = time.Parse("2006-01-02", item.GetEffectiveFrom())
		if err != nil {
			return domain.Parameter{}, status.Error(codes.InvalidArgument, "effective_from must use YYYY-MM-DD")
		}
	}
	var effectiveTo *time.Time
	if item.GetEffectiveTo() != "" {
		to, err := time.Parse("2006-01-02", item.GetEffectiveTo())
		if err != nil {
			return domain.Parameter{}, status.Error(codes.InvalidArgument, "effective_to must use YYYY-MM-DD")
		}
		effectiveTo = &to
	}
	return domain.Parameter{
		ID:            item.GetId(),
		TenantID:      ptr(item.GetTenantId()),
		Module:        item.GetModule(),
		Key:           item.GetKey(),
		Value:         item.GetValue(),
		ValueType:     item.GetValueType(),
		Unit:          item.GetUnit(),
		ScopeType:     item.GetScopeType(),
		ScopeID:       ptr(item.GetScopeId()),
		EffectiveFrom: from,
		EffectiveTo:   effectiveTo,
		Description:   ptr(item.GetDescription()),
		IsSecret:      item.GetIsSecret(),
	}, nil
}

func formatDate(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.Format("2006-01-02")
}

func lookupCategoryToProto(item domain.LookupCategory) *platformv1.LookupCategory {
	return &platformv1.LookupCategory{
		Id:          item.ID,
		TenantId:    deref(item.TenantID),
		Code:        item.Code,
		Name:        item.Name,
		ScopeType:   item.ScopeType,
		ScopeId:     deref(item.ScopeID),
		IsSystem:    item.IsSystem,
		Description: deref(item.Description),
		CreatedAt:   timestamppb.New(item.CreatedAt),
		UpdatedAt:   timestamppb.New(item.UpdatedAt),
	}
}

func lookupCategoryFromProto(item *platformv1.LookupCategory) domain.LookupCategory {
	return domain.LookupCategory{
		ID:          item.GetId(),
		TenantID:    ptr(item.GetTenantId()),
		Code:        item.GetCode(),
		Name:        item.GetName(),
		ScopeType:   item.GetScopeType(),
		ScopeID:     ptr(item.GetScopeId()),
		IsSystem:    item.GetIsSystem(),
		Description: ptr(item.GetDescription()),
	}
}

func lookupValueToProto(item domain.LookupValue) *platformv1.LookupValue {
	return &platformv1.LookupValue{
		Id:           item.ID,
		CategoryId:   item.CategoryID,
		Code:         item.Code,
		Name:         item.Name,
		SortOrder:    int32(item.SortOrder),
		IsActive:     item.IsActive,
		MetadataJson: deref(item.Metadata),
		CreatedAt:    timestamppb.New(item.CreatedAt),
		UpdatedAt:    timestamppb.New(item.UpdatedAt),
	}
}

func lookupValueFromProto(item *platformv1.LookupValue) domain.LookupValue {
	return domain.LookupValue{
		ID:         item.GetId(),
		CategoryID: item.GetCategoryId(),
		Code:       item.GetCode(),
		Name:       item.GetName(),
		SortOrder:  int(item.GetSortOrder()),
		IsActive:   item.GetIsActive(),
		Metadata:   ptr(item.GetMetadataJson()),
	}
}

func organizationToProto(item domain.Organization) *platformv1.Organization {
	return &platformv1.Organization{
		Id:            item.ID,
		TenantId:      item.TenantID,
		ParentId:      deref(item.ParentID),
		Code:          item.Code,
		Name:          item.Name,
		OrgType:       "",
		AdminUnitCode: deref(item.AdminUnitCode),
		Address:       deref(item.Address),
		IsActive:      item.IsActive,
		CreatedAt:     timestamppb.New(item.CreatedAt),
		UpdatedAt:     timestamppb.New(item.UpdatedAt),
	}
}

func organizationFromProto(item *platformv1.Organization) domain.Organization {
	return domain.Organization{
		ID:            item.GetId(),
		TenantID:      item.GetTenantId(),
		ParentID:      ptr(item.GetParentId()),
		Code:          item.GetCode(),
		Name:          item.GetName(),
		AdminUnitCode: ptr(item.GetAdminUnitCode()),
		Address:       ptr(item.GetAddress()),
		IsActive:      item.GetIsActive(),
	}
}

func adminUnitToProto(item domain.GeoAdminUnit) *platformv1.AdminUnit {
	return &platformv1.AdminUnit{
		Code:          item.Code,
		Name:          item.Name,
		FullName:      deref(item.FullName),
		ParentCode:    deref(item.ParentCode),
		Level:         int32(item.Level),
		UnitType:      item.UnitType,
		CountryCode:   item.CountryCode,
		RegionCode:    deref(item.RegionCode),
		EffectiveFrom: item.EffectiveFrom,
		EffectiveTo:   deref(item.EffectiveTo),
		IsActive:      item.IsActive,
		MetadataJson:  deref(item.Metadata),
		CreatedAt:     timestamppb.New(item.CreatedAt),
		UpdatedAt:     timestamppb.New(item.UpdatedAt),
	}
}

func adminUnitFromProto(item *platformv1.AdminUnit) domain.GeoAdminUnit {
	return domain.GeoAdminUnit{
		Code:          item.GetCode(),
		Name:          item.GetName(),
		FullName:      ptr(item.GetFullName()),
		ParentCode:    ptr(item.GetParentCode()),
		Level:         int(item.GetLevel()),
		UnitType:      item.GetUnitType(),
		CountryCode:   item.GetCountryCode(),
		RegionCode:    ptr(item.GetRegionCode()),
		EffectiveFrom: item.GetEffectiveFrom(),
		EffectiveTo:   ptr(item.GetEffectiveTo()),
		IsActive:      item.GetIsActive(),
		Metadata:      ptr(item.GetMetadataJson()),
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ptr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
