package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestNewBrailleSpinner(t *testing.T) {
	message := "Testing..."
	spinner := NewBrailleSpinner(message)

	if spinner.message != message {
		t.Errorf("Expected message %q, got %q", message, spinner.message)
	}

	if len(spinner.frames) == 0 {
		t.Error("Expected spinner to have frames")
	}

	if spinner.interval != 80*time.Millisecond {
		t.Errorf("Expected interval 80ms, got %v", spinner.interval)
	}

	if spinner.active {
		t.Error("Expected spinner to not be active initially")
	}
}

func TestBrailleSpinner_StartAndStop(t *testing.T) {
	var buf bytes.Buffer
	spinner := NewBrailleSpinner("Test")
	spinner.writer = &buf

	// Start spinner
	spinner.Start()

	// Wait a bit for spinner to run
	time.Sleep(150 * time.Millisecond)

	// Stop spinner
	spinner.Stop("Done")

	// Check that output was written
	output := buf.String()
	if len(output) == 0 {
		t.Error("Expected spinner to write output")
	}

	// Should contain the completion message
	if !strings.Contains(output, "Done") {
		t.Error("Expected output to contain completion message")
	}

	// Verify spinner is no longer active
	if spinner.active {
		t.Error("Expected spinner to be inactive after stop")
	}
}

func TestBrailleSpinner_UpdateMessage(t *testing.T) {
	var buf bytes.Buffer
	spinner := NewBrailleSpinner("Initial")
	spinner.writer = &buf

	spinner.Start()
	time.Sleep(50 * time.Millisecond)

	// Update message
	newMessage := "Updated"
	spinner.UpdateMessage(newMessage)

	time.Sleep(100 * time.Millisecond)
	spinner.Stop("")

	// Check that the updated message appears in output
	output := buf.String()
	if !strings.Contains(output, newMessage) {
		t.Errorf("Expected output to contain updated message %q", newMessage)
	}
}

func TestBrailleSpinner_DoubleStart(t *testing.T) {
	spinner := NewBrailleSpinner("Test")

	spinner.Start()
	defer spinner.Stop("")

	// Starting again should not cause panic
	spinner.Start()

	// Should still be active
	if !spinner.active {
		t.Error("Expected spinner to remain active")
	}
}

func TestBrailleSpinner_DoubleStop(t *testing.T) {
	spinner := NewBrailleSpinner("Test")

	spinner.Start()
	time.Sleep(50 * time.Millisecond)
	spinner.Stop("")

	// Stopping again should not cause panic
	spinner.Stop("")

	// Should be inactive
	if spinner.active {
		t.Error("Expected spinner to be inactive")
	}
}

func TestBrailleSpinner_StopWithoutStart(t *testing.T) {
	spinner := NewBrailleSpinner("Test")

	// Stopping without starting should not panic
	spinner.Stop("")

	if spinner.active {
		t.Error("Expected spinner to be inactive")
	}
}

func TestBrailleSpinner_FrameRotation(t *testing.T) {
	var buf bytes.Buffer
	spinner := NewBrailleSpinner("Test")
	spinner.writer = &buf

	expectedFrames := spinner.frames

	spinner.Start()

	// Wait for multiple frame cycles
	time.Sleep(time.Duration(len(expectedFrames)+2) * spinner.interval)

	spinner.Stop("")

	output := buf.String()

	// Check that at least some frames appear in output
	foundFrames := 0
	for _, frame := range expectedFrames {
		if strings.ContainsRune(output, frame) {
			foundFrames++
		}
	}

	if foundFrames < 3 {
		t.Errorf("Expected to find at least 3 different frames in output, found %d", foundFrames)
	}
}

func TestBrailleSpinner_EmptyCompletionMessage(t *testing.T) {
	var buf bytes.Buffer
	spinner := NewBrailleSpinner("Test")
	spinner.writer = &buf

	spinner.Start()
	time.Sleep(50 * time.Millisecond)

	// Stop with empty completion message
	spinner.Stop("")

	// Should still work and clear the line
	output := buf.String()
	if len(output) == 0 {
		t.Error("Expected some output from spinner")
	}
}
