package config

import "testing"

func TestLoadDefaultsGRPCToClusterPort(t *testing.T) {
	t.Setenv("CONFIG_FILE", "missing-config.yaml")
	t.Setenv("GRPC_ADDR", "")

	if got := Load().GRPCAddr; got != "0.0.0.0:9090" {
		t.Fatalf("default gRPC address = %q, want cluster port 9090", got)
	}
}
