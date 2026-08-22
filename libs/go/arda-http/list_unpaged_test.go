package ardahttp

import "testing"

func TestNewListResponseUsesEmptyItems(t *testing.T) {
	response := NewListResponse[string](1, 1, 0, nil)
	if response.Items == nil {
		t.Fatal("NewListResponse() returned nil items")
	}
}
