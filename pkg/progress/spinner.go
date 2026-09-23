package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// Spinner shows a single animated status line while something runs. When the
// writer is not a terminal nothing is shown until Stop prints the final line.
type Spinner struct {
	w    io.Writer
	text string
	tty  bool

	mu      sync.Mutex
	stop    chan struct{}
	done    chan struct{}
	started bool
}

// NewSpinner returns a spinner that writes to w with the given text.
func NewSpinner(w io.Writer, text string) *Spinner {
	tty := false
	if f, ok := w.(*os.File); ok {
		tty = term.IsTerminal(int(f.Fd()))
	}

	return &Spinner{w: w, text: text, tty: tty}
}

// Start begins animating (on a terminal).
func (s *Spinner) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started || !s.tty {
		s.started = true

		return
	}

	s.started = true
	s.stop = make(chan struct{})
	s.done = make(chan struct{})

	go s.run()
}

func (s *Spinner) run() {
	defer close(s.done)

	frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
	ticker := time.NewTicker(100 * time.Millisecond)

	defer ticker.Stop()

	_, _ = fmt.Fprint(s.w, "\x1b[?25l") // hide cursor

	for i := 0; ; i++ {
		_, _ = fmt.Fprintf(s.w, "\r\x1b[2K%c %s", frames[i%len(frames)], s.text)

		select {
		case <-s.stop:
			_, _ = fmt.Fprint(s.w, "\r\x1b[2K\x1b[?25h") // clear line, show cursor

			return
		case <-ticker.C:
		}
	}
}

// Stop ends the animation and prints final as the status line, if non-empty.
func (s *Spinner) Stop(final string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stop != nil {
		close(s.stop)
		<-s.done
		s.stop = nil
	}

	if final != "" {
		_, _ = fmt.Fprintln(s.w, final)
	}
}
