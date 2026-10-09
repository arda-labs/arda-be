package push

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestPayloadContainsOnlyGenericRoutingMetadata(t *testing.T) {
	body, err := json.Marshal(Payload{ID: "inbox-1", Href: "/workflow/inbox"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 || fields["id"] != "inbox-1" || fields["href"] != "/workflow/inbox" {
		t.Fatalf("payload contains unexpected fields: %s", body)
	}
}

func TestExpiredSubscriptionStatusesAreClassified(t *testing.T) {
	for _, status := range []int{404, 410} {
		err := statusError(status)
		if !errors.Is(err, ErrSubscriptionExpired) {
			t.Fatalf("status %d error = %v, want expired subscription", status, err)
		}
	}
	for _, status := range []int{400, 429, 500} {
		err := statusError(status)
		if err == nil || errors.Is(err, ErrSubscriptionExpired) {
			t.Fatalf("status %d error = %v, want non-expiration error", status, err)
		}
	}
	if err := statusError(201); err != nil {
		t.Fatalf("successful status returned error: %v", err)
	}
}
