package http

import (
	"github.com/arda-labs/arda/apps/mdm-service/internal/domain"
	"github.com/arda-labs/arda/apps/mdm-service/internal/handler"
	"github.com/arda-labs/arda/apps/mdm-service/internal/service"
	ardaopenapi "github.com/arda-labs/arda/libs/go/arda-openapi"
)

// OpenAPIDocument declares the public mdm REST surface. Each operation names
// the Go type its handler decodes and the type it writes as the envelope
// result, so contracts/openapi/mdm-v1.json is derived from the real wire
// shape. TestOpenAPIDocumentIsCommitted regenerates the document and fails when
// the committed copy differs; check-openapi-routes.mjs fails when a public
// route in router.go is missing here (ADR-005).
//
// The simple catalogs are registered once per entry of the service registry;
// the document describes them with a {catalog} path parameter restricted to
// service.CatalogNames(). Every catalog row shares domain.CatalogItem, with the
// catalog-specific columns validated inside `attributes`.
func OpenAPIDocument() ardaopenapi.Document {
	type (
		op    = ardaopenapi.Operation
		param = ardaopenapi.Param
	)
	list := []param{
		{Name: "page", Type: "integer"},
		{Name: "per_page", Type: "integer"},
		{Name: "sort", Enum: []string{"code", "name", "created_at"}},
		{Name: "order", Enum: []string{"asc", "desc"}},
		{Name: "q", Description: "Free-text search over code and name"},
		{Name: "all", Type: "boolean", Description: "Return every row up to the server cap instead of one page"},
		{Name: "is_active", Description: "Comma-separated filter: true, false or both"},
		{Name: "include_inactive", Type: "boolean", Description: "Legacy: include inactive rows"},
	}
	catalogs := map[string][]string{"catalog": service.CatalogNames()}
	conflict := []int{400, 401, 403, 409, 500}
	conflictOrMissing := []int{400, 401, 403, 404, 409, 500}

	return ardaopenapi.Document{
		Title:       "Arda MDM",
		Version:     "1.0.0",
		Description: "Master data catalogs and versioned interest rates. Generated from apps/mdm-service/internal/transport/http/openapi.go; do not edit by hand.",
		Operations: []op{
			// Simple catalogs (one route set per registry entry)
			{Method: "GET", Path: "/api/mdm/{catalog}", ID: "listCatalogItems", Summary: "List the rows of one catalog", PathEnums: catalogs, Query: list, Result: domain.CatalogItem{}, List: true},
			{Method: "POST", Path: "/api/mdm/{catalog}", ID: "createCatalogItem", Summary: "Create a catalog row", PathEnums: catalogs, Request: domain.CatalogItem{}, Result: domain.CatalogItem{}, Problems: conflict},
			{Method: "GET", Path: "/api/mdm/{catalog}/{id}", ID: "getCatalogItem", Summary: "Read a catalog row", PathEnums: catalogs, Result: domain.CatalogItem{}},
			{Method: "PUT", Path: "/api/mdm/{catalog}/{id}", ID: "updateCatalogItem", Summary: "Update a catalog row", PathEnums: catalogs, Request: domain.CatalogItem{}, Result: domain.CatalogItem{}, Problems: conflictOrMissing},
			{Method: "DELETE", Path: "/api/mdm/{catalog}/{id}", ID: "deleteCatalogItem", Summary: "Delete a catalog row", PathEnums: catalogs, Result: handler.Deleted{}},

			// Interest rates and their tiers
			{Method: "GET", Path: "/api/mdm/interest-rates", ID: "listInterestRates", Summary: "List interest rate headers", Query: list, Result: domain.InterestRate{}, List: true},
			{Method: "POST", Path: "/api/mdm/interest-rates", ID: "createInterestRate", Summary: "Create an interest rate header", Request: domain.InterestRate{}, Result: domain.InterestRate{}, Problems: conflict},
			{Method: "GET", Path: "/api/mdm/interest-rates/{id}", ID: "getInterestRate", Summary: "Read an interest rate header", Result: domain.InterestRate{}},
			{Method: "PUT", Path: "/api/mdm/interest-rates/{id}", ID: "updateInterestRate", Summary: "Update an interest rate header", Request: domain.InterestRate{}, Result: domain.InterestRate{}, Problems: conflictOrMissing},
			{Method: "DELETE", Path: "/api/mdm/interest-rates/{id}", ID: "deleteInterestRate", Summary: "Delete an interest rate header", Result: handler.Deleted{}},
			{Method: "GET", Path: "/api/mdm/interest-rates/{id}/tiers", ID: "listInterestRateTiers", Summary: "List the tiers of one rate", Result: []domain.InterestRateTier{}, ResultName: "InterestRateTiers"},
			{Method: "POST", Path: "/api/mdm/interest-rates/{id}/tiers", ID: "createInterestRateTier", Summary: "Add a tier to one rate", Request: domain.InterestRateTier{}, Result: domain.InterestRateTier{}, Problems: conflictOrMissing},
			{Method: "PUT", Path: "/api/mdm/interest-rates/{id}/tiers/{tierId}", ID: "updateInterestRateTier", Summary: "Update a tier", Request: domain.InterestRateTier{}, Result: domain.InterestRateTier{}, Problems: conflictOrMissing},
			{Method: "DELETE", Path: "/api/mdm/interest-rates/{id}/tiers/{tierId}", ID: "deleteInterestRateTier", Summary: "Delete a tier", Result: handler.Deleted{}},
		},
	}
}
