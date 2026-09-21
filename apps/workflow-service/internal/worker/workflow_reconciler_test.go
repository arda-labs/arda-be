package worker

import "testing"

func TestReconcileAction(t *testing.T) {
	cases := []struct {
		name      string
		state     string
		seen      bool
		hasActive bool
		want      string
	}{
		{"active task stays open", "CREATED", true, true, reconcileTouch},
		{"completed task closes", "COMPLETED", true, true, reconcileClose},
		{"canceled task closes", "CANCELED", true, true, reconcileClose},
		{"missing key with another active task closes", "", false, true, reconcileClose},
		{"missing key with no active task touches only", "", false, false, reconcileTouch},
		{"unknown state touches only", "ASSIGNED", true, false, reconcileTouch},
	}
	for _, tc := range cases {
		if got := reconcileAction(tc.state, tc.seen, tc.hasActive); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}
