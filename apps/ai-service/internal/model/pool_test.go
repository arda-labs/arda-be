package model

import (
	"testing"
)

func TestClientPool_ReusesSameClientForSameConfig(t *testing.T) {
	pool := NewClientPool(nil)

	client1 := pool.GetClient("tenant-1", "https://api.openai.com/v1", "key-1", "gpt-4o")
	client2 := pool.GetClient("tenant-1", "https://api.openai.com/v1", "key-1", "gpt-4o")

	if client1 != client2 {
		t.Fatal("expected pool to return the identical client instance for matching tenant and config")
	}

	// Different config creates new client
	client3 := pool.GetClient("tenant-1", "https://api.openai.com/v1", "key-2-new", "gpt-4o")
	if client1 == client3 {
		t.Fatal("expected new client instance when config changes")
	}

	// Invalidate removes cached client
	pool.Invalidate("tenant-1")
	client4 := pool.GetClient("tenant-1", "https://api.openai.com/v1", "key-2-new", "gpt-4o")
	if client3 == client4 {
		t.Fatal("expected new client instance after invalidate")
	}
}

func TestClientPool_MultiTenantIsolation(t *testing.T) {
	pool := NewClientPool(nil)

	clientA := pool.GetClient("tenant-A", "https://api.openai.com/v1", "key-A", "gpt-4o")
	clientB := pool.GetClient("tenant-B", "https://api.openai.com/v1", "key-B", "gpt-4o")

	if clientA == clientB {
		t.Fatal("different tenants must receive different client instances")
	}
}
