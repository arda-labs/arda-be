package http

import (
	nethttp "net/http"

	"github.com/arda-labs/arda/apps/loan-service/internal/domain"
	"github.com/arda-labs/arda/apps/loan-service/internal/handler"
	"github.com/arda-labs/arda/apps/loan-service/internal/repository"
	"github.com/arda-labs/arda/apps/loan-service/internal/service"
	loanclient "github.com/arda-labs/arda/libs/go/arda-grpc/client/loan"
	ardaopenapi "github.com/arda-labs/arda/libs/go/arda-openapi"
)

// OpenAPIDocument declares the public loan REST surface. Each operation names
// the Go type its handler decodes and the type it writes as the envelope
// result, so contracts/openapi/loan-v1.json is derived from the real wire
// shape. TestOpenAPIDocumentIsCommitted regenerates the document and fails when
// the committed copy differs; check-openapi-routes.mjs fails when a public
// route in router.go is missing here (ADR-005).
//
// Adjustment flows are registered once per kind in loanclient.Kinds; the
// document describes them with a {kind} path parameter restricted to that list.
func OpenAPIDocument() ardaopenapi.Document {
	type (
		op    = ardaopenapi.Operation
		param = ardaopenapi.Param
	)
	created, accepted := nethttp.StatusCreated, nethttp.StatusAccepted
	list := []param{
		{Name: "page", Type: "integer"},
		{Name: "per_page", Type: "integer"},
		{Name: "sort"},
		{Name: "order", Enum: []string{"asc", "desc"}},
		{Name: "q", Description: "Free-text search"},
		{Name: "all", Type: "boolean", Description: "Return every row up to the server cap instead of one page"},
	}
	with := func(extra ...param) []param { return append(append([]param{}, extra...), list...) }
	kinds := map[string][]string{"kind": loanclient.Kinds}
	period := []param{{Name: "from", Description: "Start date, YYYY-MM-DD"}, {Name: "to", Description: "End date, YYYY-MM-DD"}}
	contractCode := param{Name: "contract_code"}
	status := param{Name: "status"}

	return ardaopenapi.Document{
		Title:       "Arda Loan",
		Version:     "1.0.0",
		Description: "Loan contracts, agreements, collateral, disbursements, collections, adjustments, provisions, batches, plans and reports. Generated from apps/loan-service/internal/transport/http/openapi.go; do not edit by hand.",
		Operations: []op{
			// Contracts
			{Method: "GET", Path: "/api/loan/contracts", ID: "listLoanContracts", Summary: "List loan contracts", Query: with(status), Result: domain.Contract{}, List: true},
			{Method: "POST", Path: "/api/loan/contracts", ID: "createLoanContract", Summary: "Create a loan contract", Request: domain.Contract{}, Result: domain.Contract{}},
			{Method: "GET", Path: "/api/loan/contracts/{id}", ID: "getLoanContract", Summary: "Read a loan contract", Result: domain.Contract{}},
			{Method: "PUT", Path: "/api/loan/contracts/{id}", ID: "updateLoanContract", Summary: "Revise a draft, pending or rejected contract", Request: domain.Contract{}, Result: domain.Contract{}},
			{Method: "POST", Path: "/api/loan/contracts/{id}/submit", ID: "submitLoanContract", Summary: "Submit the contract approval case", Result: domain.Contract{}},
			{Method: "GET", Path: "/api/loan/contracts/{id}/dossier", ID: "getLoanContractDossier", Summary: "Read the composite contract dossier", Result: repository.Dossier{}},

			// Agreements, repayment schedules, collateral
			{Method: "GET", Path: "/api/loan/agreements", ID: "listLoanAgreements", Summary: "List loan agreements", Query: with(contractCode), Result: domain.Agreement{}, List: true},
			{Method: "POST", Path: "/api/loan/agreements", ID: "createLoanAgreement", Summary: "Create a loan agreement", Request: domain.Agreement{}, Result: domain.Agreement{}},
			{Method: "GET", Path: "/api/loan/repay-plans", ID: "listLoanRepayPlans", Summary: "List repayment schedule rows", Query: []param{contractCode, {Name: "agreement_code"}}, Result: domain.RepayPlan{}, List: true},
			{Method: "GET", Path: "/api/loan/mortgages", ID: "listLoanMortgages", Summary: "List mortgage contracts", Query: list, Result: domain.Mortgage{}, List: true},
			{Method: "POST", Path: "/api/loan/mortgages", ID: "createLoanMortgage", Summary: "Create a mortgage contract", Request: domain.Mortgage{}, Result: domain.Mortgage{}},
			{Method: "GET", Path: "/api/loan/collaterals", ID: "listLoanCollaterals", Summary: "List collateral assets", Query: with(param{Name: "mortgage_code"}), Result: domain.Collateral{}, List: true},
			{Method: "POST", Path: "/api/loan/collaterals", ID: "createLoanCollateral", Summary: "Register a collateral asset", Request: domain.Collateral{}, Result: domain.Collateral{}},
			{Method: "GET", Path: "/api/loan/contract-collaterals", ID: "listLoanContractCollaterals", Summary: "List collateral attached to contracts", Query: with(contractCode), Result: domain.ContractCollateral{}, List: true},
			{Method: "POST", Path: "/api/loan/contract-collaterals", ID: "attachLoanContractCollateral", Summary: "Attach collateral to a contract", Request: domain.ContractCollateral{}, Result: domain.ContractCollateral{}},

			// Adjustments (one uniform set per kind)
			{Method: "GET", Path: "/api/loan/adjustments/{kind}", ID: "listLoanAdjustments", Summary: "List adjustments of one kind", PathEnums: kinds, Query: with(contractCode, status), Result: domain.Adjustment{}, List: true},
			{Method: "POST", Path: "/api/loan/adjustments/{kind}", ID: "createLoanAdjustment", Summary: "Create an adjustment of one kind", PathEnums: kinds, Request: domain.Adjustment{}, Result: domain.Adjustment{}},
			{Method: "GET", Path: "/api/loan/adjustments/{kind}/{id}", ID: "getLoanAdjustment", Summary: "Read an adjustment", PathEnums: kinds, Result: domain.Adjustment{}},
			{Method: "POST", Path: "/api/loan/adjustments/{kind}/{id}/submit", ID: "submitLoanAdjustment", Summary: "Submit the adjustment approval case", PathEnums: kinds, Result: domain.Adjustment{}},

			// Products and VFU (ủy thác)
			{Method: "GET", Path: "/api/loan/products", ID: "listLoanProducts", Summary: "List loan products", Query: with(param{Name: "include_inactive", Type: "boolean"}, param{Name: "is_active", Description: "CSV of true/false"}), Result: domain.LoanProduct{}, List: true},
			{Method: "POST", Path: "/api/loan/products", ID: "createLoanProduct", Summary: "Create or update a loan product", Request: domain.LoanProduct{}, Result: domain.LoanProduct{}},
			{Method: "PUT", Path: "/api/loan/products", ID: "updateLoanProduct", Summary: "Create or update a loan product", Request: domain.LoanProduct{}, Result: domain.LoanProduct{}},
			{Method: "GET", Path: "/api/loan/vfu/parties", ID: "listVfuParties", Summary: "List VFU parties", Query: list, Result: domain.VfuParty{}, List: true},
			{Method: "POST", Path: "/api/loan/vfu/parties", ID: "createVfuParty", Summary: "Create a VFU party", Request: domain.VfuParty{}, Result: domain.VfuParty{}},
			{Method: "PUT", Path: "/api/loan/vfu/parties/{id}", ID: "updateVfuParty", Summary: "Update a VFU party", Request: domain.VfuParty{}, Result: domain.VfuParty{}},
			{Method: "GET", Path: "/api/loan/vfu/mandates", ID: "listVfuMandates", Summary: "List VFU mandates", Query: list, Result: domain.VfuMandate{}, List: true},
			{Method: "POST", Path: "/api/loan/vfu/mandates", ID: "createVfuMandate", Summary: "Create a VFU mandate", Request: domain.VfuMandate{}, Result: domain.VfuMandate{}},
			{Method: "PUT", Path: "/api/loan/vfu/mandates/{id}", ID: "updateVfuMandate", Summary: "Update a VFU mandate", Request: domain.VfuMandate{}, Result: domain.VfuMandate{}},
			{Method: "GET", Path: "/api/loan/vfu/plans", ID: "listVfuPlans", Summary: "List VFU plans", Query: with(param{Name: "mandate_code"}), Result: domain.VfuPlan{}, List: true},
			{Method: "POST", Path: "/api/loan/vfu/plans", ID: "createVfuPlan", Summary: "Create a VFU plan", Request: domain.VfuPlan{}, Result: domain.VfuPlan{}},
			{Method: "PUT", Path: "/api/loan/vfu/plans/{id}", ID: "updateVfuPlan", Summary: "Update a VFU plan", Request: domain.VfuPlan{}, Result: domain.VfuPlan{}},

			// Disbursements and collections
			{Method: "GET", Path: "/api/loan/disbursements", ID: "listLoanDisbursements", Summary: "List disbursements", Query: with(status, contractCode, param{Name: "flow_type", Enum: []string{"REGISTER", "COMPLETE"}}), Result: domain.Disbursement{}, List: true},
			{Method: "POST", Path: "/api/loan/disbursements", ID: "createLoanDisbursement", Summary: "Create a disbursement", Request: domain.Disbursement{}, Result: domain.Disbursement{}},
			{Method: "GET", Path: "/api/loan/disbursements/{id}", ID: "getLoanDisbursement", Summary: "Read a disbursement", Result: domain.Disbursement{}},
			{Method: "POST", Path: "/api/loan/disbursements/{id}/submit", ID: "submitLoanDisbursement", Summary: "Submit the disbursement approval case", Result: domain.Disbursement{}},
			{Method: "GET", Path: "/api/loan/collections", ID: "listLoanCollections", Summary: "List collections", Query: with(status, contractCode), Result: domain.Collection{}, List: true},
			{Method: "POST", Path: "/api/loan/collections", ID: "createLoanCollection", Summary: "Create a collection", Request: domain.Collection{}, Result: domain.Collection{}},
			{Method: "GET", Path: "/api/loan/collections/{id}", ID: "getLoanCollection", Summary: "Read a collection", Result: domain.Collection{}},
			{Method: "POST", Path: "/api/loan/collections/{id}/submit", ID: "submitLoanCollection", Summary: "Submit the collection approval case", Result: domain.Collection{}},
			{Method: "GET", Path: "/api/loan/accruals", ID: "listLoanAccruals", Summary: "List interest accruals", Query: list, Result: domain.Accrual{}, List: true},

			// Provisions
			{Method: "GET", Path: "/api/loan/general-provisions", ID: "listGeneralProvisions", Summary: "List general provisions", Query: []param{{Name: "org", Description: "Organization code"}}, Result: repository.GeneralProvisionRow{}, List: true},
			{Method: "POST", Path: "/api/loan/general-provisions", ID: "submitGeneralProvision", Summary: "Submit a general provision for approval", Request: handler.GeneralProvisionRequest{}, Result: repository.GeneralProvisionRow{}},
			{Method: "GET", Path: "/api/loan/general-provisions/{id}", ID: "getGeneralProvision", Summary: "Read a general provision", Result: repository.GeneralProvisionRow{}},
			{Method: "POST", Path: "/api/loan/general-provisions/calculate", ID: "calculateGeneralProvision", Summary: "Preview a general provision", Request: handler.GeneralProvisionRequest{}, Result: service.GeneralProvisionPreview{}},
			{Method: "GET", Path: "/api/loan/specific-provisions", ID: "listSpecificProvisions", Summary: "List specific provisions", Query: []param{status}, Result: repository.SpecificProvisionRow{}, List: true},
			{Method: "POST", Path: "/api/loan/specific-provisions", ID: "submitSpecificProvision", Summary: "Submit a specific provision for approval", Request: handler.SpecificProvisionRequest{}, Result: repository.SpecificProvisionRow{}},
			{Method: "GET", Path: "/api/loan/specific-provisions/{id}", ID: "getSpecificProvision", Summary: "Read a specific provision", Result: repository.SpecificProvisionRow{}},
			{Method: "POST", Path: "/api/loan/specific-provisions/calculate", ID: "calculateSpecificProvision", Summary: "Preview a specific provision", Request: handler.SpecificProvisionRequest{}, Result: service.SpecificProvisionPreview{}},

			// Batches (1 hồ sơ — N hợp đồng)
			{Method: "GET", Path: "/api/loan/disbursement-batches", ID: "listDisbursementBatches", Summary: "List disbursement batches", Query: []param{status, {Name: "flow_type", Enum: []string{"REGISTER", "COMPLETE"}}, list[0], list[1], list[2], list[3]}, Result: domain.DisbursementBatch{}, List: true},
			{Method: "POST", Path: "/api/loan/disbursement-batches", ID: "createDisbursementBatchDraft", Summary: "Create a REGISTER disbursement batch draft", Request: service.CreateBatchInput{}, Result: handler.BatchDraftCreated{}, Status: created},
			{Method: "POST", Path: "/api/loan/disbursement-batches/complete", ID: "createDisbursementBatchComplete", Summary: "Create a COMPLETE batch from a posted REGISTER batch", Request: handler.BatchCompleteRequest{}, Result: handler.BatchDraftCreated{}, Status: created},
			{Method: "GET", Path: "/api/loan/disbursement-batches/{id}", ID: "getDisbursementBatch", Summary: "Read a disbursement batch with its rows", Result: domain.DisbursementBatch{}},
			{Method: "PUT", Path: "/api/loan/disbursement-batches/{id}", ID: "updateDisbursementBatchDraft", Summary: "Update a DRAFT batch", Request: service.CreateBatchInput{}, Result: domain.DisbursementBatch{}},
			{Method: "POST", Path: "/api/loan/disbursement-batches/{id}/submit", ID: "submitDisbursementBatch", Summary: "Queue the batch approval case", Request: handler.BatchVersionRequest{}, Result: handler.BatchSubmitAccepted{}, Status: accepted},
			{Method: "POST", Path: "/api/loan/disbursement-batches/{id}/preview", ID: "previewDisbursementBatchPosting", Summary: "Validate the batch posting without reserving or posting", Result: handler.BatchPostingPreview{}, Problems: []int{400, 401, 403, 404, 409, 422, 500, 503}},
			{Method: "POST", Path: "/api/loan/disbursement-batches/{id}/cancel", ID: "cancelDisbursementBatchDraft", Summary: "Cancel a batch draft", Request: handler.BatchVersionRequest{}, Result: handler.BatchCancelled{}},
			{Method: "GET", Path: "/api/loan/collection-batches", ID: "listCollectionBatches", Summary: "List collection batches", Query: []param{status, list[0], list[1], list[2], list[3]}, Result: domain.CollectionBatch{}, List: true},
			{Method: "POST", Path: "/api/loan/collection-batches", ID: "createCollectionBatch", Summary: "Create and submit a collection batch", Request: service.CreateBatchInputCollection{}, Result: domain.CollectionBatch{}},
			{Method: "GET", Path: "/api/loan/collection-batches/{id}", ID: "getCollectionBatch", Summary: "Read a collection batch with its rows", Result: domain.CollectionBatch{}},
			{Method: "GET", Path: "/api/loan/posting-rules", ID: "listLoanPostingRules", Summary: "Preview the finance posting rule card of a loan document type",
				Query:  []param{{Name: "document_type", Required: true, Enum: []string{"LNM_DISB_REGISTER", "LNM_DISB_COMPLETE", "LNM_COLLECTION", "LNM_ACCRUAL", "LNM_PROVISION"}}},
				Result: handler.PostingRules{}},

			// Plans
			{Method: "GET", Path: "/api/loan/plans", ID: "listLoanPlans", Summary: "List loan plans", Query: []param{status}, Result: repository.LoanPlan{}, List: true},
			{Method: "POST", Path: "/api/loan/plans", ID: "createLoanPlan", Summary: "Create or update a loan plan", Request: repository.LoanPlan{}, Result: repository.LoanPlan{}, Status: created},
			{Method: "PUT", Path: "/api/loan/plans", ID: "updateLoanPlan", Summary: "Create or update a loan plan", Request: repository.LoanPlan{}, Result: repository.LoanPlan{}, Status: created},
			{Method: "DELETE", Path: "/api/loan/plans/{id}", ID: "closeLoanPlan", Summary: "Close a loan plan", Result: handler.PlanClosed{}},

			// Reports
			{Method: "GET", Path: "/api/loan/reports/loan-ledger", ID: "getLoanLedgerReport", Summary: "Loan ledger report", Query: append(append([]param{}, period...), contractCode), Result: repository.LoanLedgerRow{}, List: true},
			{Method: "GET", Path: "/api/loan/reports/loan-statement", ID: "getLoanStatementReport", Summary: "Loan statement report", Query: []param{contractCode}, Result: repository.LoanStatementRow{}, List: true},
			{Method: "GET", Path: "/api/loan/reports/collateral-statement", ID: "getCollateralStatementReport", Summary: "Collateral statement report", Query: []param{status}, Result: repository.CollateralStatementRow{}, List: true},
			{Method: "GET", Path: "/api/loan/reports/loan-diary", ID: "getLoanDiaryReport", Summary: "Loan diary report", Query: append(append([]param{}, period...), contractCode), Result: repository.LoanDiaryRow{}, List: true},
			{Method: "GET", Path: "/api/loan/reports/loan-appraisal", ID: "getLoanAppraisalReport", Summary: "Loan appraisal report", Query: []param{contractCode}, Result: repository.LoanAppraisalRow{}, List: true},
			{Method: "GET", Path: "/api/loan/reports/loan-reconciliation", ID: "getLoanReconciliationReport", Summary: "Loan reconciliation report", Query: []param{contractCode}, Result: repository.LoanReconciliationRow{}, List: true},
		},
	}
}
