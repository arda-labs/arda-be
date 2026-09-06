package events

import (
	"os"
	"testing"

	ardaleakcheck "github.com/arda-labs/arda/libs/go/arda-leakcheck"
)

func TestMain(m *testing.M) {
	os.Exit(ardaleakcheck.Run(m))
}
