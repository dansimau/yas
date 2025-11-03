// Package progress provides utilities for running goroutines in parallel with CLI progress display.
package progress

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/hashicorp/go-multierror"
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
	Name       string
	Fn         func() error
	StatusLine string
}

// Runner manages parallel goroutine execution with progress display.
type Runner struct {
	maxGoroutines int
	header        string
	tasks         []Task
	results       []TaskResult
	mu            sync.Mutex

	// Status tracking
	pending     map[string]bool
	running     map[string]bool
	completed   map[string]error
	statusLines map[string]string
}

// New creates a new Runner with the specified max goroutines and header.
func New(maxGoroutines int, header string) *Runner {
	return &Runner{
		maxGoroutines: maxGoroutines,
		header:        header,
		tasks:         []Task{},
		results:       []TaskResult{},
		pending:       make(map[string]bool),
		running:       make(map[string]bool),
		completed:     make(map[string]error),
		statusLines:   make(map[string]string),
	}
}

// Add adds a task to the runner.
func (r *Runner) Add(name string, fn func() error) {
	r.tasks = append(r.tasks, Task{Name: name, Fn: fn})
	r.pending[name] = true
}

// UpdateStatusLine updates the status line for a task.
func (r *Runner) UpdateStatusLine(name, statusLine string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.statusLines[name] = statusLine
}

// Start executes all tasks in parallel and displays progress.
// If printResults is true, prints the final results after completion.
func (r *Runner) Start(printResults bool) error {
	if len(r.tasks) == 0 {
		return nil
	}

	// Print header if provided
	if r.header != "" {
		fmt.Printf("\x1b[1m%s\x1b[0m\n", r.header)
	}

	// Hide cursor during execution
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h") // Show cursor on exit

	// Print initial status for all tasks
	for _, task := range r.tasks {
		fmt.Printf("⌛ %s\n", task.Name)
	}

	// Start spinner goroutine
	stopSpinner := make(chan struct{})

	spinnerDone := make(chan struct{})
	go r.spinner(stopSpinner, spinnerDone)

	// Execute tasks in parallel
	p := pool.New().WithMaxGoroutines(r.maxGoroutines)
	for i := range r.tasks {
		task := r.tasks[i]

		p.Go(func() {
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
				exitErr := &exec.ExitError{}
				if errors.As(err, &exitErr) {
					stderr = string(exitErr.Stderr)
				}
			}

			r.results = append(r.results, TaskResult{
				Name:   task.Name,
				Error:  err,
				Stderr: stderr,
			})
			r.mu.Unlock()
		})
	}

	// Wait for all tasks to complete
	p.Wait()

	// Stop spinner and wait for it to fully exit
	close(stopSpinner)
	<-spinnerDone

	if printResults {
		// Print final results
		r.printFinalResults()
	} else {
		// Clear all status lines
		r.clearStatusLines()
	}

	// Collect errors from task results
	var result error

	for _, taskResult := range r.results {
		if taskResult.Error != nil {
			result = multierror.Append(result, taskResult.Error)
		}
	}

	return result
}

// clearStatusLines clears all status lines from the terminal.
func (r *Runner) clearStatusLines() {
	n := len(r.tasks)

	// Move cursor up n lines to first task line
	fmt.Printf("\x1b[%dA", n)

	// Clear each line and move down
	for range r.tasks {
		fmt.Print("\x1b[2K") // Clear entire line
		fmt.Print("\x1b[B")  // Move down one line
	}

	// Move cursor back up to first task line
	fmt.Printf("\x1b[%dA", n)
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

	n := len(r.tasks)

	// Move cursor up n lines to first task line
	fmt.Printf("\x1b[%dA", n)

	// Redraw each task's status
	for _, task := range r.tasks {
		fmt.Print("\x1b[2K") // Clear entire line

		var icon string

		if err, completed := r.completed[task.Name]; completed {
			if err != nil {
				icon = "❌"
			} else {
				icon = "☑️"
			}
		} else if r.running[task.Name] {
			icon = " " + string(spinnerChar)
		} else {
			icon = "⌛"
		}

		// Format task name with optional status line
		taskDisplay := task.Name
		if statusLine, hasStatus := r.statusLines[task.Name]; hasStatus && statusLine != "" {
			taskDisplay = task.Name + ": " + statusLine
		}

		fmt.Printf("%s %s\n", icon, taskDisplay)
	}
	// Cursor is now at start of line after all tasks
}

// printFinalResults clears the status lines and prints final results.
func (r *Runner) printFinalResults() {
	n := len(r.tasks)

	// Move cursor up n lines to first task line
	fmt.Printf("\x1b[%dA", n)

	// Print final results for each task
	for _, task := range r.tasks {
		err := r.completed[task.Name]

		// Format task name with optional status line
		taskDisplay := task.Name
		if statusLine, hasStatus := r.statusLines[task.Name]; hasStatus && statusLine != "" {
			taskDisplay = task.Name + ": " + statusLine
		}

		if err != nil {
			// Failed task - print with error icon and stderr
			fmt.Printf("❌ %s\n", taskDisplay)

			// Find the stderr for this task
			for _, result := range r.results {
				if result.Name == task.Name && result.Stderr != "" {
					// Print stderr in red
					fmt.Printf("\x1b[31m%s\x1b[0m", result.Stderr)

					if result.Stderr[len(result.Stderr)-1] != '\n' {
						fmt.Println()
					}

					break
				}
			}
		} else {
			// Successful task
			fmt.Printf("☑️ %s\n", taskDisplay)
		}
	}
}
