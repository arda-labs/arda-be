package events

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

type testDirectory struct{ iam, role, org, fallback []string }

func (d testDirectory) ResolveNotificationRecipients(_ context.Context, _ string, users, groups, roles []string) ([]string, error) {
	if len(groups) > 0 {
		return d.fallback, nil
	}
	if len(roles) > 0 {
		return d.role, nil
	}
	if len(users) > 0 || len(groups) > 0 {
		return d.iam, nil
	}
	return d.iam, nil
}
func (d testDirectory) ListIAMUsersByOrgUnit(context.Context, string, string, bool) ([]string, error) {
	return d.org, nil
}

func TestResolveRecipientsRoleAndOrgUnitIntersect(t *testing.T) {
	got, err := ResolveRecipients(context.Background(), testDirectory{iam: []string{" u1 "}, role: []string{"u2", "u3"}, org: []string{"u2", "u4"}}, "tenant", RecipientSelectors{UserIDs: []string{"u1"}, RoleCodes: []string{"LNM_CHECKER"}, OrgUnitIDs: []string{"unit"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"u1", "u2"}) {
		t.Fatalf("recipients = %v, want [u1 u2]", got)
	}
}

func TestResolveRecipientsUsesConfiguredOverflowGroup(t *testing.T) {
	ids := make([]string, 1001)
	for i := range ids {
		ids[i] = fmt.Sprintf("user-%04d", i)
	}
	got, err := ResolveRecipients(context.Background(), testDirectory{iam: ids, fallback: []string{"ops"}}, "tenant", RecipientSelectors{UserIDs: []string{"user"}}, "NOTIFICATION_MANAGERS")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"ops"}) {
		t.Fatalf("recipients = %v, want [ops]", got)
	}
}

func TestResolveRecipientsEmptyDoesNotFallback(t *testing.T) {
	got, err := ResolveRecipients(context.Background(), testDirectory{}, "tenant", RecipientSelectors{RoleCodes: []string{"MISSING"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("recipients = %v, want empty", got)
	}
}
