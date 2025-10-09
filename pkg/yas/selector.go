package yas

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// SelectionItem represents an item in the interactive selector
type SelectionItem struct {
	ID   string // The identifier (e.g., branch name for checkout)
	Line string // The formatted display line (with colors, status, etc.)
}

// InteractiveSelect displays an interactive list and returns the selected item
// Users can navigate with arrow keys, select with Enter, or cancel with Escape
// Returns nil if the user cancels
// initialCursor specifies the initial selection index
// header is displayed in bold at the top (empty string for no header)
func InteractiveSelect(items []SelectionItem, initialCursor int, header string) (*SelectionItem, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no items to select from")
	}

	// Validate initial cursor
	cursor := initialCursor
	if cursor < 0 || cursor >= len(items) {
		cursor = 0
	}

	// Put terminal into raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil, fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Hide cursor during selection
	fmt.Print("\x1b[?25l")
	defer fmt.Print("\x1b[?25h") // Show cursor on exit

	// Print header if provided
	headerLines := 0
	if header != "" {
		fmt.Printf("\x1b[1m%s\x1b[0m\r\n", header)
		headerLines = 1
	}

	render(items, cursor)

	// Read input
	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("failed to read input: %w", err)
		}

		// Handle key presses
		switch {
		case n == 1 && buf[0] == 27: // Escape
			clearLines(len(items) + headerLines)
			return nil, nil

		case n == 1 && buf[0] == 3: // Ctrl-C
			clearLines(len(items) + headerLines)
			return nil, nil

		case n == 1 && (buf[0] == 13 || buf[0] == 10): // Enter
			clearLines(len(items) + headerLines)
			return &items[cursor], nil

		case n == 3 && buf[0] == 27 && buf[1] == 91 && buf[2] == 65: // Up arrow
			if cursor > 0 {
				cursor--
				redraw(items, cursor, headerLines)
			}

		case n == 3 && buf[0] == 27 && buf[1] == 91 && buf[2] == 66: // Down arrow
			if cursor < len(items)-1 {
				cursor++
				redraw(items, cursor, headerLines)
			}
		}
	}

	return nil, nil
}

// render displays all items with the cursor at the current position
func render(items []SelectionItem, cursor int) {
	for i, item := range items {
		if i == cursor {
			// Bold the selector character
			fmt.Printf("\x1b[1m>\x1b[0m %s\r\n", item.Line)
		} else {
			fmt.Printf("  %s\r\n", item.Line)
		}
	}
	// Cursor is now one line below all items (less distracting)
}

// redraw clears the current display and redraws with the new cursor position
func redraw(items []SelectionItem, cursor int, headerLines int) {
	// Move cursor up to first item line (we're one line below items, skip header)
	for range len(items) {
		fmt.Print("\x1b[A")
	}
	// Clear and redraw all item lines
	for i, item := range items {
		fmt.Print("\r\x1b[2K") // Clear entire line
		if i == cursor {
			// Bold the selector character
			fmt.Printf("\x1b[1m>\x1b[0m %s", item.Line)
		} else {
			fmt.Printf("  %s", item.Line)
		}
		if i < len(items)-1 {
			fmt.Print("\r\n") // Move to next line
		}
	}
	// Move cursor back to one line below items
	fmt.Print("\r\n")
}

// clearLines clears n lines from the terminal and returns cursor to original position
// Assumes cursor is one line below the n items
func clearLines(n int) {
	// Move up n lines to first item line
	for range n {
		fmt.Print("\x1b[A")
	}
	// Clear all n lines
	for i := 0; i < n; i++ {
		fmt.Print("\r\x1b[2K") // Clear current line
		if i < n-1 {
			fmt.Print("\x1b[B") // Move down one line (no newline)
		}
	}
	// We're at line n-1, move back up to line 0 (original position)
	for i := 0; i < n-1; i++ {
		fmt.Print("\x1b[A")
	}
	fmt.Print("\r") // Ensure at column 0
}
