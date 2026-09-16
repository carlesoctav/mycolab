package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

// ErrCancelled is returned when the user aborts the picker.
var ErrCancelled = errors.New("cancelled")

// Select shows an interactive single-select list and returns the chosen index.
// Keys: j/k or arrow keys to move, g/G for first/last, Enter to confirm,
// q/Esc/Ctrl-C to cancel. stdin must be a terminal. The cursor starts on
// the first item; use SelectWithDefault to start elsewhere.
func Select(title string, lines []string) (int, error) {
	return SelectWithDefault(title, lines, 0)
}

// SelectWithDefault is Select with an initial cursor position (clamped).
func SelectWithDefault(title string, lines []string, start int) (int, error) {
	if len(lines) == 0 {
		return -1, fmt.Errorf("nothing to select")
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return -1, fmt.Errorf("no interactive terminal available")
	}
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return -1, err
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		restored = true
		_ = term.Restore(fd, oldState)
		fmt.Print("\033[?25h")
	}
	defer restore()

	// Best effort: restore the terminal if killed from outside.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		if _, ok := <-sigCh; ok {
			restore()
			os.Exit(1)
		}
	}()

	fmt.Print("\033[?25l")   // hide cursor
	height := len(lines) + 1 // rows + footer
	if title != "" {
		height++
	}
	selected := start
	if selected < 0 {
		selected = 0
	}
	if selected > len(lines)-1 {
		selected = len(lines) - 1
	}
	render := func(first bool) {
		var b strings.Builder
		if !first {
			fmt.Fprintf(&b, "\033[%dA", height)
		}
		if title != "" {
			fmt.Fprintf(&b, "\r\033[K  %s\r\n", title)
		}
		for i, line := range lines {
			if i == selected {
				fmt.Fprintf(&b, "\r\033[K\033[1;36m  ❯ %s\033[0m\r\n", line)
			} else {
				fmt.Fprintf(&b, "\r\033[K    %s\r\n", line)
			}
		}
		fmt.Fprintf(&b, "\r\033[K  \033[90m(j/k to move, enter to select, q to cancel)\033[0m")
		fmt.Print(b.String())
	}
	render(true)

	for {
		key, err := readKey(fd)
		if err != nil {
			return -1, err
		}
		switch key {
		case "up":
			if selected > 0 {
				selected--
				render(false)
			}
		case "down":
			if selected < len(lines)-1 {
				selected++
				render(false)
			}
		case "top":
			if selected != 0 {
				selected = 0
				render(false)
			}
		case "bottom":
			if selected != len(lines)-1 {
				selected = len(lines) - 1
				render(false)
			}
		case "enter":
			fmt.Print("\r\n")
			return selected, nil
		case "cancel":
			fmt.Print("\r\n")
			return -1, ErrCancelled
		}
	}
}

// readKey translates raw input bytes into an action name.
func readKey(fd int) (string, error) {
	buf := make([]byte, 1)
	if _, err := os.Stdin.Read(buf); err != nil {
		return "", err
	}
	switch buf[0] {
	case 'j', 'J':
		return "down", nil
	case 'k', 'K':
		return "up", nil
	case 'g':
		return "top", nil
	case 'G':
		return "bottom", nil
	case '\r', '\n':
		return "enter", nil
	case 'q', 'Q', 0x03: // q, Ctrl-C
		return "cancel", nil
	case 0x1b: // Esc or an escape sequence (arrows)
		return readEscape(fd)
	}
	return "", nil
}

// readEscape distinguishes a bare Esc (cancel) from an arrow-key sequence.
// Arrow keys arrive as ESC [ A (up) / ESC [ B (down); a bare Esc arrives alone.
func readEscape(fd int) (string, error) {
	type byteResult struct {
		b   byte
		err error
	}
	next := make(chan byteResult, 1)
	go func() {
		var buf [1]byte
		_, err := os.Stdin.Read(buf[:])
		next <- byteResult{buf[0], err}
	}()
	select {
	case r := <-next:
		if r.err != nil || r.b != '[' {
			return "cancel", r.err
		}
	case <-time.After(50 * time.Millisecond):
		return "cancel", nil
	}
	var final [1]byte
	if _, err := os.Stdin.Read(final[:]); err != nil {
		return "", err
	}
	switch final[0] {
	case 'A':
		return "up", nil
	case 'B':
		return "down", nil
	}
	return "", nil
}
