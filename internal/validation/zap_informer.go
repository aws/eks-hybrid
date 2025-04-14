package validation

import (
	"context"

	"go.uber.org/zap"
)

// ZapInformer is an informer that uses a zap logger to print validation starts.
type ZapInformer struct {
	logger *zap.Logger
}

// NewZapInformer creates a new ZapInformer that uses the provided logger.
func NewZapInformer(logger *zap.Logger) *ZapInformer {
	return &ZapInformer{
		logger: logger.WithOptions(zap.AddCallerSkip(1)),
	}
}

// Starting logs the start of a validation using the zap logger.
func (z *ZapInformer) Starting(ctx context.Context, name, message string) {
	z.logger.Info(message)
}

// Done is a no-op for ZapInformer as we only care about validation starts.
func (z *ZapInformer) Done(ctx context.Context, name string, err error) {
	// No-op as we only care about validation starts
}
