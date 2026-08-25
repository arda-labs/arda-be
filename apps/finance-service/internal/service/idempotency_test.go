package service

import "testing"

func TestVerifyIdempotentReplayRejectsDifferentOrMissingHash(t *testing.T) {
	for _, tc := range []struct {
		name string
		old  string
		new  string
	}{
		{name: "different", old: "abc", new: "def"},
		{name: "legacy missing", old: "", new: "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := verifyIdempotentReplay(tc.old, tc.new); err != ErrIdempotencyConflict {
				t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
			}
		})
	}
}

func TestVerifyIdempotentReplayAcceptsSameHash(t *testing.T) {
	if err := verifyIdempotentReplay("abc", "abc"); err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
}
