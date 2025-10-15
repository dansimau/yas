// Package progress provides utilities for running goroutines in parallel with CLI progress display.
package progress

import (
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/sourcegraph/conc/pool"
)

// TaskResult holds the result of a task execution.
type TaskResult struct {
	Name   string
	Error  error
	Stderr string // populated if error is exec.ExitError
}

// Task represents a unit of work to be executed.
type Task struct {
	Name string
	Fn   func() error
}

// Runner manages parallel goroutine execution with progress display.
type Runner struct {
	maxGoroutines int
	tasks         []Task
	results       []TaskResult
	mu            sync.Mutex

	// Status tracking
	pending   map[string]bool
	running   map[string]bool
	completed map[string]error
}

// New creates a new Runner with the specified max goroutines.
func New(maxGoroutines int) *Runner {
	return &Runner{
		maxGoroutines: maxGoroutines,
		tasks:         []Task{},
		results:       []TaskResult{},
		pending:       make(map[string]bool),
		running:       make(map[string]bool),
		completed:     make(map[string]error),
	}
}

// Add adds a task to the runner.
func (r *Runner) Add(name string, fn func() error) {
	r.tasks = append(r.tasks, Task{Name: name, Fn: fn})
	r.pending[name] = true
}

// Start executes all tasks in parallel and displays progress.
func (r *Runner) Start() error {
	if len(r.tasks) == 0 {
		return nil
	}

	// Hide cursor during execution
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h") // Show cursor on exit

	// Print initial status for all tasks
	for _, task := range r.tasks {
		fmt.Printf("⌛ %s\r\n", task.Name)
	}

	// Start spinner goroutine
	stopSpinner := make(chan struct{})
	spinnerDone := make(chan struct{})
	go r.spinner(stopSpinner, spinnerDone)

	// Execute tasks in parallel
	p := pool.New().WithMaxGoroutines(r.maxGoroutines).WithErrors()
	for _, task := range r.tasks {
		task := task // capture for closure
		p.Go(func() error {
			// Mark as running
			r.mu.Lock()
			delete(r.pending, task.Name)
			r.running[task.Name] = true
			r.mu.Unlock()

			// Execute the task
			err := task.Fn()

			// Mark as completed
			r.mu.Lock()
			delete(r.running, task.Name)
			r.completed[task.Name] = err

			// Capture stderr if it's an exec error
			stderr := ""
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					stderr = string(exitErr.Stderr)
				}
			}

			r.results = append(r.results, TaskResult{
				Name:   task.Name,
				Error:  err,
				Stderr: stderr,
			})
			r.mu.Unlock()

			return nil // Don't propagate errors to pool
		})
	}

	// Wait for all tasks to complete
	_ = p.Wait()

	// Stop spinner
	close(stopSpinner)
	<-spinnerDone

	// Clear all status lines
	r.clearStatusLines()

	return nil
}

// Results returns the results of all executed tasks.
func (r *Runner) Results() []TaskResult {
	return r.results
}

// spinner updates the progress display periodically.
func (r *Runner) spinner(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)

	spinnerChars := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
	spinnerIdx := 0

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			r.updateDisplay(spinnerChars[spinnerIdx])
			spinnerIdx = (spinnerIdx + 1) % len(spinnerChars)
		}
	}
}

// updateDisplay refreshes the status display.
func (r *Runner) updateDisplay(spinnerChar rune) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Move cursor to start of status lines (up len(tasks) lines)
	for range r.tasks {
		fmt.Print("\x1b[A")
	}

	// Redraw each task's status
	for i, task := range r.tasks {
		fmt.Print("\r\x1b[2K") // Clear entire line

		var icon string
		if err, completed := r.completed[task.Name]; completed {
			if err != nil {
				icon = "❌"
			} else {
				icon = "☑️"
			}
		} else if r.running[task.Name] {
			icon = string(spinnerChar)
		} else {
			icon = "⌛"
		}

		fmt.Printf("%s %s", icon, task.Name)

		if i < len(r.tasks)-1 {
			fmt.Print("\r\n")
		}
	}

	// Move cursor to one line below all tasks
	fmt.Print("\r\n")
}

// clearStatusLines clears all status lines from the terminal.
func (r *Runner) clearStatusLines() {
	// Move up len(tasks) lines to first status line
	for range r.tasks {
		fmt.Print("\x1b[A")
	}

	// Clear all status lines
	for i := range r.tasks {
		fmt.Print("\r\x1b[2K") // Clear current line

		if i < len(r.tasks)-1 {
			fmt.Print("\x1b[B") // Move down one line (no newline)
		}
	}

	// Move back up to line 0 (original position)
	for range len(r.tasks) - 1 {
		fmt.Print("\x1b[A")
	}

	fmt.Print("\r") // Ensure at column 0
}
