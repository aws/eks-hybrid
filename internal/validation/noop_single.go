package validation

import "context"

// NoopSingleRunner is a single runner that performs no validations.
type NoopSingleRunner[O Validatable[O]] struct{}

// NewNoopSingleRunner constructs a new NoopSingleRunner.
func NewNoopSingleRunner[O Validatable[O]]() *NoopSingleRunner[O] {
	return &NoopSingleRunner[O]{}
}

// Run is a no-op implementation that always returns nil.
func (r *NoopSingleRunner[O]) Run(ctx context.Context, obj O, validations ...Validation[O]) error {
	return nil
}
