package config

import "testing"

func TestGRPCAddrForKnownAndUnknownOwners(t *testing.T) {
	cfg := Config{HRMGRPCAddr: "hrm-service:9090", DepositGRPCAddr: "deposit-service:9090"}
	if got := cfg.GRPCAddrFor("hrm-service"); got != "hrm-service:9090" {
		t.Fatalf("hrm addr = %q", got)
	}
	if got := cfg.GRPCAddrFor(" deposit-service "); got != "deposit-service:9090" {
		t.Fatalf("deposit addr = %q", got)
	}
	if got := cfg.GRPCAddrFor("unknown-service"); got != "" {
		t.Fatalf("unknown owner addr = %q, want empty", got)
	}
}
