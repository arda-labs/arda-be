package main

import "testing"

func TestValidateInputsRequiresSingleReplayConfirmation(t *testing.T) {
	if err := validateInputs("outbox-1", "operator-1", confirmation, "postgres://redacted"); err != nil {
		t.Fatalf("valid replay inputs rejected: %v", err)
	}
	for name, args := range map[string][4]string{
		"missing outbox":       {"", "operator-1", confirmation, "dsn"},
		"missing operator":     {"outbox-1", "", confirmation, "dsn"},
		"missing confirmation": {"outbox-1", "operator-1", "", "dsn"},
		"missing DSN":          {"outbox-1", "operator-1", confirmation, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateInputs(args[0], args[1], args[2], args[3]); err == nil {
				t.Fatal("invalid replay inputs were accepted")
			}
		})
	}
}
