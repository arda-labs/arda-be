package policy

import "testing"

func TestABACMatchRequiresExplicitActiveStatus(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]any
		want bool
	}{
		{name: "missing status", env: map[string]any{}, want: false},
		{name: "inactive status", env: map[string]any{"user_status": "DISABLED"}, want: false},
		{name: "malformed status", env: map[string]any{"user_status": true}, want: false},
		{name: "active status", env: map[string]any{"user_status": "ACTIVE"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := abacMatchFunc(tt.env, "user-1")
			if err != nil {
				t.Fatalf("abacMatchFunc returned error: %v", err)
			}
			allowed, ok := got.(bool)
			if !ok || allowed != tt.want {
				t.Fatalf("abacMatchFunc = %#v, want %v", got, tt.want)
			}
		})
	}
}

func TestABACMatchRequiresSubject(t *testing.T) {
	got, err := abacMatchFunc(map[string]any{"user_status": "ACTIVE"}, "")
	if err != nil {
		t.Fatalf("abacMatchFunc returned error: %v", err)
	}
	if allowed, _ := got.(bool); allowed {
		t.Fatal("abacMatchFunc allowed an empty subject")
	}
}
