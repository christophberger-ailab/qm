package cli

import (
	"errors"
	"testing"

	"github.com/christophberger/start"
)

// A failing command must be recorded, because start.Up() prints the error
// and returns without setting an exit status — and Quarto reads the
// pre-render hook's exit status to decide whether to abort the render.
func TestGuardRecordsFailure(t *testing.T) {
	t.Cleanup(func() { failed.Store(false) })

	ok := Guard(func(*start.Command) error { return nil })
	if err := ok(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if failed.Load() {
		t.Fatal("a successful command was recorded as failed")
	}

	want := errors.New("boom")
	bad := Guard(func(*start.Command) error { return want })
	if err := bad(nil); !errors.Is(err, want) {
		t.Fatalf("error = %v, want it passed through to start", err)
	}
	if !failed.Load() {
		t.Fatal("a failing command was not recorded")
	}
}
