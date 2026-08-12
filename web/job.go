package web

import (
	"fmt"
	"sync"

	"github.com/cboct/qm/internal/bookrender"
)

// job is the single background render the server runs at a time. The tree
// handlers must stay responsive while Quarto works, which can take minutes,
// so the render runs in its own goroutine and the UI polls this for output.
type job struct {
	mu      sync.Mutex
	lines   []string
	running bool
	failed  bool
}

// jobState is a consistent snapshot of a job for rendering.
type jobState struct {
	Lines   []string
	Running bool
	Failed  bool
}

func (j *job) state() jobState {
	j.mu.Lock()
	defer j.mu.Unlock()
	return jobState{
		Lines:   append([]string(nil), j.lines...),
		Running: j.running,
		Failed:  j.failed,
	}
}

func (j *job) logf(format string, args ...any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, fmt.Sprintf(format, args...))
}

func (j *job) fail() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.failed = true
}

// start begins a render in the background, reporting whether it was
// accepted: a job already running is left alone.
func (j *job) start(o bookrender.Options) bool {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return false
	}
	j.running, j.failed, j.lines = true, false, nil
	j.mu.Unlock()

	go func() {
		if err := bookrender.Run(o, j.logf); err != nil {
			j.fail()
		}
		j.mu.Lock()
		j.running = false
		j.mu.Unlock()
	}()
	return true
}
