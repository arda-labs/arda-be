package model

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCircuitOpensAfterPreOutputFailures(t *testing.T) {
	f := &chainFake{err: errors.New("down")}
	c := NewCircuitBreakerProvider(f, 2, time.Minute)
	for i := 0; i < 2; i++ {
		_, _, _ = c.StreamChat(context.Background(), nil, nil, StreamCallbacks{})
	}
	if _, _, err := c.StreamChat(context.Background(), nil, nil, StreamCallbacks{}); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected open circuit, got %v", err)
	}
}

func TestCircuitDoesNotCountPartialFailure(t *testing.T) {
	f := &chainFake{err: errors.New("disconnect"), output: "partial"}
	c := NewCircuitBreakerProvider(f, 1, time.Minute)
	_, _, _ = c.StreamChat(context.Background(), nil, nil, StreamCallbacks{})
	if _, _, err := c.StreamChat(context.Background(), nil, nil, StreamCallbacks{}); err == ErrCircuitOpen {
		t.Fatal("partial output failure must not open circuit")
	}
}
