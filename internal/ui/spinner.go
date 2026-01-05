package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// BrailleSpinner provides a Unicode braille pattern spinner for indicating progress
type BrailleSpinner struct {
	frames   []rune
	message  string
	interval time.Duration
	writer   io.Writer
	stop     chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	active   bool
}

// NewBrailleSpinner creates a new braille spinner with the given message
func NewBrailleSpinner(message string) *BrailleSpinner {
	return &BrailleSpinner{
		frames:   []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'},
		message:  message,
		interval: 80 * time.Millisecond,
		writer:   os.Stdout,
		stop:     make(chan struct{}),
	}
}

// Start begins the spinner animation
func (s *BrailleSpinner) Start() {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		frameIndex := 0
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.mu.Lock()
				frame := s.frames[frameIndex]
				fmt.Fprintf(s.writer, "\r%c %s", frame, s.message)
				frameIndex = (frameIndex + 1) % len(s.frames)
				s.mu.Unlock()
			}
		}
	}()
}

// Stop stops the spinner and optionally shows a completion message
func (s *BrailleSpinner) Stop(completionMessage string) {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	s.mu.Unlock()

	close(s.stop)
	s.wg.Wait()

	// Clear the line and show completion message
	if completionMessage != "" {
		fmt.Fprintf(s.writer, "\r%s\n", completionMessage)
	} else {
		fmt.Fprintf(s.writer, "\r")
	}
}

// UpdateMessage changes the spinner's message while it's running
func (s *BrailleSpinner) UpdateMessage(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = message
}
