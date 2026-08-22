package ardahttp

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteUnpagedList(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteUnpagedList(recorder, httptest.NewRequest("GET", "/items", nil), []string{"one", "two"})

	var response ListResponse[string]
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Page != 1 || response.PerPage != 2 || response.Total != 2 {
		t.Fatalf("unexpected pagination metadata: %+v", response)
	}
}
