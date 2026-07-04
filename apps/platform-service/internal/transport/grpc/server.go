package grpc

import (
	"context"

	"github.com/arda-labs/arda/apps/platform-service/internal/domain"
	"github.com/arda-labs/arda/apps/platform-service/internal/repository"
	"github.com/arda-labs/arda/apps/platform-service/internal/service"
	platformv1 "github.com/arda-labs/arda/libs/go/arda-proto/platform/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type PlatformServer struct {
	platformv1.UnimplementedPlatformServiceServer
	svc *service.PlatformService
}

func NewPlatformServer(svc *service.PlatformService) *PlatformServer {
	return &PlatformServer{svc: svc}
}

func (s *PlatformServer) ListParameters(ctx context.Context, req *platformv1.ListParametersRequest) (*platformv1.ListParametersResponse, error) {
	scope := req.GetScope()
	items, err := s.svc.ListParameters(ctx, scope.GetTenantId(), scope.GetScopeType(), scope.GetScopeId())
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
	item, err := s.svc.UpsertParameter(ctx, parameterFromProto(req.GetParameter()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return parameterToProto(item), nil
}

func (s *PlatformServer) ResolveParameter(ctx context.Context, req *platformv1.ResolveParameterRequest) (*platformv1.Parameter, error) {
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}
	scopes := make([]service.ScopeSelector, 0, len(req.GetScopes()))
	for _, scope := range req.GetScopes() {
		scopes = append(scopes, service.ScopeSelector{
			TenantID:  scope.GetTenantId(),
			ScopeType: scope.GetScopeType(),
			ScopeID:   scope.GetScopeId(),
		})
	}
	item, err := s.svc.ResolveParameter(ctx, req.GetTenantId(), req.GetKey(), scopes)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return parameterToProto(item), nil
}

func (s *PlatformServer) ListLookupCategories(ctx context.Context, req *platformv1.ListLookupCategoriesRequest) (*platformv1.ListLookupCategoriesResponse, error) {
	scope := req.GetScope()
	items, err := s.svc.ListLookupCategories(ctx, scope.GetTenantId(), scope.GetScopeType(), scope.GetScopeId())
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
	item, err := s.svc.UpsertLookupCategory(ctx, lookupCategoryFromProto(req.GetCategory()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return lookupCategoryToProto(item), nil
}

func (s *PlatformServer) ListLookupValues(ctx context.Context, req *platformv1.ListLookupValuesRequest) (*platformv1.ListLookupValuesResponse, error) {
	if req.GetCategoryCode() == "" {
		return nil, status.Error(codes.InvalidArgument, "category_code is required")
	}
	items, err := s.svc.ListLookupValues(ctx, req.GetCategoryCode())
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
	item, err := s.svc.UpsertLookupValue(ctx, req.GetCategoryCode(), lookupValueFromProto(req.GetValue()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return lookupValueToProto(item), nil
}

func (s *PlatformServer) ListOrganizations(ctx context.Context, req *platformv1.ListOrganizationsRequest) (*platformv1.ListOrganizationsResponse, error) {
	items, total, err := s.svc.ListOrganizations(ctx, repository.ListOrganizationsParams{
		TenantID: req.GetTenantId(),
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
	item, err := s.svc.CreateOrganization(ctx, organizationFromProto(req.GetOrganization()))
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
		Id:          item.ID,
		TenantId:    deref(item.TenantID),
		Key:         item.Key,
		Value:       item.Value,
		ValueType:   item.ValueType,
		ScopeType:   item.ScopeType,
		ScopeId:     deref(item.ScopeID),
		Description: deref(item.Description),
		IsSecret:    item.IsSecret,
		CreatedAt:   timestamppb.New(item.CreatedAt),
		UpdatedAt:   timestamppb.New(item.UpdatedAt),
	}
}

func parameterFromProto(item *platformv1.Parameter) domain.Parameter {
	return domain.Parameter{
		ID:          item.GetId(),
		TenantID:    ptr(item.GetTenantId()),
		Key:         item.GetKey(),
		Value:       item.GetValue(),
		ValueType:   item.GetValueType(),
		ScopeType:   item.GetScopeType(),
		ScopeID:     ptr(item.GetScopeId()),
		Description: ptr(item.GetDescription()),
		IsSecret:    item.GetIsSecret(),
	}
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
