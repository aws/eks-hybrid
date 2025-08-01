package validation

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap/zaptest"
)

func TestLoggerPrinter_Starting(t *testing.T) {
	logger := zaptest.NewLogger(t)
	printer := NewLoggerPrinterWithLogger(logger)
	ctx := context.Background()

	// Should not panic
	printer.Starting(ctx, "test-validation", "Testing validation message")
}

func TestLoggerPrinter_Done_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	printer := NewLoggerPrinterWithLogger(logger)
	ctx := context.Background()

	// Should not panic on success
	printer.Done(ctx, "test-validation", nil)
}

func TestLoggerPrinter_Done_Error(t *testing.T) {
	logger := zaptest.NewLogger(t)
	printer := NewLoggerPrinterWithLogger(logger)
	ctx := context.Background()

	err := errors.New("test error")

	// Should not panic on error
	printer.Done(ctx, "test-validation", err)
}

func TestLoggerPrinter_Done_RemediableError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	printer := NewLoggerPrinterWithLogger(logger)
	ctx := context.Background()

	err := NewRemediableErr("test error", "test remediation")

	// Should not panic on remediable error
	printer.Done(ctx, "test-validation", err)
}

func TestLoggerPrinter_NewLoggerPrinter(t *testing.T) {
	ctx := context.Background()

	// Test creating printer from context
	printer := NewLoggerPrinter(ctx)
	if printer == nil {
		t.Fatal("Expected non-nil printer")
	}

	// Should not panic when using the printer
	printer.Starting(ctx, "test", "test message")
	printer.Done(ctx, "test", nil)
}

func TestLoggerPrinter_MultipleErrors(t *testing.T) {
	logger := zaptest.NewLogger(t)
	printer := NewLoggerPrinterWithLogger(logger)
	ctx := context.Background()

	// Create a joined error with multiple sub-errors
	err1 := errors.New("first error")
	err2 := NewRemediableErr("second error", "fix this")
	joinedErr := errors.Join(err1, err2)

	// Should handle multiple errors without panicking
	printer.Done(ctx, "test-validation", joinedErr)
}
