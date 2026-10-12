package http

import (
	nethttp "net/http"

	"github.com/arda-labs/arda/apps/deposit-service/internal/handler"
	"github.com/arda-labs/arda/apps/deposit-service/internal/repository"
	"github.com/arda-labs/arda/apps/deposit-service/internal/service"
	ardaopenapi "github.com/arda-labs/arda/libs/go/arda-openapi"
)

// OpenAPIDocument declares the public deposit REST surface. Each operation
// names the Go type its handler decodes and the type it writes as the
// envelope result, so contracts/openapi/deposit-v1.json is derived from the
// real wire shape. TestOpenAPIDocumentIsCommitted regenerates the document and
// fails when the committed copy differs; check-openapi-routes.mjs fails when a
// public route in router.go is missing here (ADR-005).
func OpenAPIDocument() ardaopenapi.Document {
	created := nethttp.StatusCreated
	status := []ardaopenapi.Param{{Name: "status", Description: "Filter by status"}}
	period := []ardaopenapi.Param{
		{Name: "from", Description: "Start date, YYYY-MM-DD"},
		{Name: "to", Description: "End date, YYYY-MM-DD"},
	}
	return ardaopenapi.Document{
		Title:       "Arda Deposit",
		Version:     "1.0.0",
		Description: "Savings, deposit products, interest rates and interbank placements and borrowings. Generated from apps/deposit-service/internal/transport/http/openapi.go; do not edit by hand.",
		Operations: []ardaopenapi.Operation{
			{Method: "GET", Path: "/api/deposit/products", ID: "listDepositProducts", Summary: "List savings products",
				Query:  []ardaopenapi.Param{{Name: "q"}, {Name: "sort"}, {Name: "order", Enum: []string{"asc", "desc"}}, {Name: "is_active", Type: "boolean"}},
				Result: repository.SavingsProduct{}, List: true},
			{Method: "POST", Path: "/api/deposit/products", ID: "createDepositProduct", Summary: "Create or update a savings product",
				Request: repository.SavingsProduct{}, Result: repository.SavingsProduct{}, Status: created},
			{Method: "PUT", Path: "/api/deposit/products", ID: "updateDepositProduct", Summary: "Create or update a savings product",
				Request: repository.SavingsProduct{}, Result: repository.SavingsProduct{}, Status: created},

			{Method: "GET", Path: "/api/deposit/product-requests", ID: "listDepositProductRequests", Summary: "List staged product requests",
				Query: status, Result: repository.ProductRequest{}, List: true},
			{Method: "POST", Path: "/api/deposit/product-requests", ID: "submitDepositProductRequest", Summary: "Stage a product request and submit its approval case",
				Request: service.ProductRequestInput{}, Result: service.Submission{}, Status: created},
			{Method: "GET", Path: "/api/deposit/product-requests/{id}", ID: "getDepositProductRequest", Summary: "Read a staged product request",
				Result: repository.ProductRequest{}},

			{Method: "GET", Path: "/api/deposit/savings", ID: "listSavings", Summary: "List savings accounts",
				Query: []ardaopenapi.Param{{Name: "status"}, {Name: "q"}}, Result: repository.Savings{}, List: true},
			{Method: "POST", Path: "/api/deposit/savings/open", ID: "openSavings", Summary: "Open a savings account",
				Request: service.OpenSavingsInput{}, Result: repository.Savings{}, Status: created},
			{Method: "GET", Path: "/api/deposit/savings/{code}", ID: "getSavingsDetail", Summary: "Read a savings account with its interest state",
				Result: service.SavingsDetail{}},
			{Method: "POST", Path: "/api/deposit/savings/{code}/settle", ID: "submitSavingsSettlement", Summary: "Submit the settlement approval case",
				Result: service.Submission{}, Status: created},
			{Method: "POST", Path: "/api/deposit/savings/{code}/deposit", ID: "submitAdditionalDeposit", Summary: "Submit an additional deposit approval case",
				Request: handler.AdditionalDepositRequest{}, Result: service.Submission{}, Status: created},
			{Method: "POST", Path: "/api/deposit/savings/{code}/interest", ID: "submitSavingsInterest", Summary: "Stage an interest operation on one savings account",
				Request: handler.SavingsInterestRequest{}, Result: repository.InterestOp{}, Status: created},

			{Method: "GET", Path: "/api/deposit/rates", ID: "listDepositInterestRates", Summary: "List interest rates",
				Query: []ardaopenapi.Param{{Name: "product_code"}}, Result: repository.InterestRate{}, List: true},
			{Method: "POST", Path: "/api/deposit/rates", ID: "submitDepositRateRequest", Summary: "Stage a rate change and submit its approval case",
				Request: handler.RateChangeRequest{}, Result: repository.RateRequest{}, Status: created},
			{Method: "GET", Path: "/api/deposit/rates/{id}", ID: "getDepositRateRequest", Summary: "Read a staged rate change",
				Result: repository.RateRequest{}},
			{Method: "GET", Path: "/api/deposit/interest-ops", ID: "listInterestOpsByCase", Summary: "List the interest operations of an approval case",
				Query:  []ardaopenapi.Param{{Name: "case_id", Required: true}},
				Result: repository.InterestOp{}, List: true},
			{Method: "POST", Path: "/api/deposit/batch-interest", ID: "submitBatchInterest", Summary: "Stage the batch interest case over every savings account with accrued interest",
				Result: handler.BatchInterestResult{}, Status: created},

			{Method: "GET", Path: "/api/deposit/reports/deposit-statement", ID: "getDepositStatement", Summary: "Savings statement report",
				Query: append(append([]ardaopenapi.Param{}, period...), status...), Result: repository.SavingsStatementRow{}, List: true},
			{Method: "GET", Path: "/api/deposit/reports/deposit-transactions", ID: "getDepositTransactions", Summary: "Savings transaction report",
				Query: append(append([]ardaopenapi.Param{}, period...), ardaopenapi.Param{Name: "txn_type"}), Result: repository.SavingsTxnRow{}, List: true},
			{Method: "GET", Path: "/api/deposit/reports/interbank-statement", ID: "getInterbankStatement", Summary: "Interbank placement statement report",
				Query: append(append([]ardaopenapi.Param{}, period...), status...), Result: repository.InterbankStatementRow{}, List: true},
			{Method: "GET", Path: "/api/deposit/reports/interbank-transactions", ID: "getInterbankTransactions", Summary: "Interbank movement report",
				Query: period, Result: repository.InterbankTxnRow{}, List: true},

			{Method: "GET", Path: "/api/deposit/interbank", ID: "listInterbankDeposits", Summary: "List interbank placements",
				Query: status, Result: repository.InterbankDeposit{}, List: true},
			{Method: "POST", Path: "/api/deposit/interbank", ID: "createInterbankDeposit", Summary: "Stage an interbank placement case",
				Request: repository.InterbankDeposit{}, Result: repository.InterbankDeposit{}, Status: created},
			{Method: "GET", Path: "/api/deposit/interbank/{id}", ID: "getInterbankDetail", Summary: "Read an interbank placement with its movements",
				Result: service.IBMDetail{}},
			{Method: "POST", Path: "/api/deposit/interbank/{id}/movements", ID: "submitInterbankMovement", Summary: "Stage a placement movement case",
				Request: handler.IBMMovementRequest{}, Result: repository.IBMMovement{}, Status: created},

			{Method: "GET", Path: "/api/deposit/borrows", ID: "listInterbankBorrows", Summary: "List interbank borrowings",
				Query: status, Result: repository.InterbankBorrow{}, List: true},
			{Method: "POST", Path: "/api/deposit/borrows", ID: "createInterbankBorrow", Summary: "Stage an interbank borrowing",
				Request: repository.InterbankBorrow{}, Result: repository.InterbankBorrow{}, Status: created},
			{Method: "GET", Path: "/api/deposit/borrows/{id}", ID: "getInterbankBorrowDetail", Summary: "Read an interbank borrowing with its movements",
				Result: handler.BorrowDetail{}},
			{Method: "POST", Path: "/api/deposit/borrows/{id}/decision", ID: "decideInterbankBorrow", Summary: "Approve or reject a staged borrowing",
				Request: handler.BorrowDecisionRequest{}, Result: repository.InterbankBorrow{}},
			{Method: "POST", Path: "/api/deposit/borrows/{id}/movements", ID: "submitInterbankBorrowMovement", Summary: "Record a borrowing movement",
				Request: repository.IBMBorrowMovement{}, Result: repository.IBMBorrowMovement{}, Status: created},

			{Method: "GET", Path: "/api/deposit/ibm-products", ID: "listInterbankProducts", Summary: "List interbank products",
				Query:  []ardaopenapi.Param{{Name: "include_inactive", Type: "boolean"}},
				Result: repository.IBMProduct{}, List: true},
			{Method: "POST", Path: "/api/deposit/ibm-products", ID: "createInterbankProduct", Summary: "Create or update an interbank product",
				Request: repository.IBMProduct{}, Result: repository.IBMProduct{}, Status: created},
			{Method: "PUT", Path: "/api/deposit/ibm-products", ID: "updateInterbankProduct", Summary: "Create or update an interbank product",
				Request: repository.IBMProduct{}, Result: repository.IBMProduct{}, Status: created},
		},
	}
}
