package outbox

import (
	"errors"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
)

type fakeStreams struct {
	infoErr error
	addErr  error
	added   []*nats.StreamConfig
}

func (f *fakeStreams) StreamInfo(string, ...nats.JSOpt) (*nats.StreamInfo, error) {
	if f.infoErr != nil {
		return nil, f.infoErr
	}
	return &nats.StreamInfo{}, nil
}

func (f *fakeStreams) AddStream(cfg *nats.StreamConfig, _ ...nats.JSOpt) (*nats.StreamInfo, error) {
	f.added = append(f.added, cfg)
	return &nats.StreamInfo{}, f.addErr
}

func TestEnsureStreamKeepsProvisionedStream(t *testing.T) {
	streams := &fakeStreams{}
	if err := EnsureStream(streams); err != nil {
		t.Fatal(err)
	}
	if len(streams.added) != 0 {
		t.Fatal("EnsureStream modified an existing stream")
	}
}

func TestEnsureStreamCreatesMissingStreamWithoutCatchAll(t *testing.T) {
	streams := &fakeStreams{infoErr: nats.ErrStreamNotFound}
	if err := EnsureStream(streams); err != nil {
		t.Fatal(err)
	}
	if len(streams.added) != 1 {
		t.Fatalf("AddStream calls = %d, want 1", len(streams.added))
	}
	cfg := streams.added[0]
	if cfg.Name != StreamName || cfg.Storage != nats.FileStorage || cfg.Retention != nats.LimitsPolicy {
		t.Fatalf("created stream config = %+v", cfg)
	}
	for _, subject := range cfg.Subjects {
		if subject == "arda.>" || strings.HasPrefix("arda.ai.", strings.TrimSuffix(subject, ">")) {
			t.Fatalf("subject %q overlaps the AI_EVENTS stream (arda.ai.*)", subject)
		}
	}
}

func TestEnsureStreamToleratesConcurrentCreation(t *testing.T) {
	streams := &fakeStreams{infoErr: nats.ErrStreamNotFound, addErr: nats.ErrStreamNameAlreadyInUse}
	if err := EnsureStream(streams); err != nil {
		t.Fatalf("EnsureStream after a concurrent create = %v", err)
	}
}

func TestEnsureStreamReportsOverlapAndInspectionErrors(t *testing.T) {
	overlap := errors.New("nats: subjects overlap with an existing stream")
	if err := EnsureStream(&fakeStreams{infoErr: nats.ErrStreamNotFound, addErr: overlap}); err == nil || !errors.Is(err, overlap) {
		t.Fatalf("overlap error = %v", err)
	}
	broken := errors.New("jetstream unavailable")
	streams := &fakeStreams{infoErr: broken}
	if err := EnsureStream(streams); err == nil || !errors.Is(err, broken) || len(streams.added) != 0 {
		t.Fatalf("inspection error = %v, added=%d", err, len(streams.added))
	}
}
