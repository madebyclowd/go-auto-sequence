package storetest_test

import (
	"testing"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/memstore"
	"github.com/madebyclowd/go-auto-sequence/storetest"
)

// A third-party store wires the suite into its own tests in a few lines.
func ExampleRun() {
	var t *testing.T // provided by the test function in real use
	storetest.Run(t, func(*testing.T) sequence.Store { return memstore.New() })
}
