package validation

import (
	"context"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/logger"
)

// LoggerPrinter is an informer that uses zap logger for validation output
// while preserving remediation functionality.
type LoggerPrinter struct {
	logger *zap.Logger
}

var _ Informer = (*LoggerPrinter)(nil)

// NewLoggerPrinter creates a new LoggerPrinter that uses the zap logger
// from the provided context.
func NewLoggerPrinter(ctx context.Context) *LoggerPrinter {
	return &LoggerPrinter{
		logger: logger.FromContext(ctx),
	}
}

// NewLoggerPrinterWithLogger creates a new LoggerPrinter with the provided logger.
func NewLoggerPrinterWithLogger(log *zap.Logger) *LoggerPrinter {
	return &LoggerPrinter{
		logger: log,
	}
}

// Starting logs the start of a validation using the zap logger.
func (p *LoggerPrinter) Starting(ctx context.Context, name, message string) {
	p.logger.Info("Starting validation",
		zap.String("validation", name),
		zap.String("message", message),
	)
}

// Done logs the result of a validation using the zap logger.
// For successful validations, it logs at Info level.
// For failed validations, it logs at Error level and includes remediation if available.
func (p *LoggerPrinter) Done(ctx context.Context, name string, err error) {
	if err == nil {
		p.logger.Info("Validation passed",
			zap.String("validation", name),
		)
		return
	}

	// Handle multiple errors if present
	errs := Unwrap(err)
	for _, e := range errs {
		p.logErrorWithRemediation(name, e)
	}
}

// logErrorWithRemediation logs an individual error and its remediation if available.
func (p *LoggerPrinter) logErrorWithRemediation(validationName string, err error) {
	// Prepare log fields
	fields := []zap.Field{
		zap.String("validation", validationName),
		zap.String("error", err.Error()),
	}

	// Add remediation to the same log entry if available
	if IsRemediable(err) {
		remediation := Remediation(err)
		fields = append(fields, zap.String("remediation", remediation))
	}

	// Log the validation failure with error and remediation in same message
	p.logger.Error("Validation failed", fields...)
}
