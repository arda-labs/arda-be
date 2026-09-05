package model

import (
	"context"
	"errors"
	"testing"
)

type chainFake struct {
	err    error
	output string
	calls  int
}

func (f *chainFake) StreamChat(_ context.Context, _ []Message, _ []ToolDef, cb StreamCallbacks) (string, Usage, error) {
	f.calls++
	if f.output != "" && cb.OnTextDelta != nil {
		cb.OnTextDelta(f.output)
	}
	return "", Usage{}, f.err
}

func TestChainFallsBackBeforeOutput(t *testing.T) {
	a, b := &chainFake{err: errors.New("down")}, &chainFake{output: "ok"}
	chain := NewChainProvider(a, b)
	var got string
	_, _, err := chain.StreamChat(context.Background(), nil, nil, StreamCallbacks{OnTextDelta: func(s string) { got += s }})
	if err != nil || got != "ok" || a.calls != 1 || b.calls != 1 {
		t.Fatalf("fallback failed err=%v got=%q calls=%d/%d", err, got, a.calls, b.calls)
	}
}

func TestChainStopsAfterPartialOutput(t *testing.T) {
	a, b := &chainFake{err: errors.New("disconnect"), output: "partial"}, &chainFake{output: "fallback"}
	chain := NewChainProvider(a, b)
	var got string
	_, _, err := chain.StreamChat(context.Background(), nil, nil, StreamCallbacks{OnTextDelta: func(s string) { got += s }})
	if err == nil || got != "partial" || b.calls != 0 {
		t.Fatalf("must stop after partial output err=%v got=%q calls=%d", err, got, b.calls)
	}
}
