package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// AP-TEN-01: danh sách khoản giải ngân/thu nợ phải fail-closed khi thiếu phạm
// vi đơn vị — không bao giờ coi "không có org" là "thấy mọi đơn vị của tenant".
// Handler không có service: nếu nhánh chặn không chạy, lời gọi service sẽ panic.
func TestLoanListsFailClosedWithoutOrgScope(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		orgIDs  string
		active  string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "collections thiếu X-User-Org-Ids", path: "/api/loan/collections", handler: (&CollectionHandler{}).ListCollections},
		{name: "disbursements thiếu X-User-Org-Ids", path: "/api/loan/disbursements", handler: (&DisbursementHandler{}).ListDisbursements},
		{name: "collections đơn vị hoạt động ngoài phạm vi", path: "/api/loan/collections", orgIDs: "ORG-01", active: "ORG-02", handler: (&CollectionHandler{}).ListCollections},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.Header.Set("X-Tenant-Id", "tenant-1")
			if tc.orgIDs != "" {
				req.Header.Set("X-User-Org-Ids", tc.orgIDs)
			}
			if tc.active != "" {
				req.Header.Set("X-Org-Id", tc.active)
			}
			res := httptest.NewRecorder()
			tc.handler(res, req)
			if res.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", res.Code, res.Body.String())
			}
		})
	}
}

func TestOrgListFilterNarrowsToAllowedOrgs(t *testing.T) {
	got, err := OrgScope{OrgIDs: []string{"ORG-01", "ORG-02"}}.ListFilter()
	if err != nil || !reflect.DeepEqual(got, []string{"ORG-01", "ORG-02"}) {
		t.Fatalf("membership filter = %v, %v", got, err)
	}
	got, err = OrgScope{ActiveOrgID: "ORG-02", OrgIDs: []string{"ORG-01", "ORG-02"}}.ListFilter()
	if err != nil || !reflect.DeepEqual(got, []string{"ORG-02"}) {
		t.Fatalf("active org filter = %v, %v", got, err)
	}
	if got, err := (OrgScope{}).ListFilter(); err == nil || got != nil {
		t.Fatalf("empty scope must fail closed, got %v, %v", got, err)
	}
}
