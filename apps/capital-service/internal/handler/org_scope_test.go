package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// AP-TEN-01: danh sách hợp đồng vốn phải fail-closed khi thiếu phạm vi đơn vị.
// Handler không có service: nếu nhánh chặn không chạy, lời gọi service sẽ panic.
func TestCapitalListsFailClosedWithoutOrgScope(t *testing.T) {
	cases := []struct {
		name    string
		orgIDs  string
		active  string
		handler func(http.ResponseWriter, *http.Request)
	}{
		{name: "contracts thiếu X-User-Org-Ids", handler: (&CapitalHandler{}).ListContracts},
		{name: "contracts đơn vị hoạt động ngoài phạm vi", orgIDs: "ORG-01", active: "ORG-02", handler: (&CapitalHandler{}).ListContracts},
		{name: "AI contracts thiếu X-User-Org-Ids", handler: (&InternalAIHandler{}).InternalAIListContracts},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/capital/contracts", nil)
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
	got, err := capOrgScope{OrgIDs: []string{"ORG-01", "ORG-02"}}.listFilter()
	if err != nil || !reflect.DeepEqual(got, []string{"ORG-01", "ORG-02"}) {
		t.Fatalf("membership filter = %v, %v", got, err)
	}
	got, err = capOrgScope{ActiveOrgID: "ORG-02", OrgIDs: []string{"ORG-01", "ORG-02"}}.listFilter()
	if err != nil || !reflect.DeepEqual(got, []string{"ORG-02"}) {
		t.Fatalf("active org filter = %v, %v", got, err)
	}
	if got, err := (capOrgScope{}).listFilter(); err == nil || got != nil {
		t.Fatalf("empty scope must fail closed, got %v, %v", got, err)
	}
}
