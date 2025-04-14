package validation

import (
	"context"
	"reflect"
	"slices"
)

// SingleRunnerConfig holds the configuration for the SingleRunner.
type SingleRunnerConfig struct {
	skipValidations []string
}

// SingleRunnerOpt allows to configure the SingleRunner.
type SingleRunnerOpt func(*SingleRunnerConfig)

// WithSingleRunnerSkipValidations configures the runner to skip
// the validations with the given names.
func WithSingleRunnerSkipValidations(namesToSkip ...string) SingleRunnerOpt {
	return func(c *SingleRunnerConfig) {
		c.skipValidations = append(c.skipValidations, namesToSkip...)
	}
}

// SingleRunner is a runner that executes a single validation.
type SingleRunner[O Validatable[O]] struct {
	informer Informer
	config   SingleRunnerConfig
}

// NewSingleRunner constructs a new SingleRunner.
func NewSingleRunner[O Validatable[O]](informer Informer, opts ...SingleRunnerOpt) *SingleRunner[O] {
	r := &SingleRunner[O]{
		informer: informer,
	}

	for _, opt := range opts {
		opt(&r.config)
	}

	return r
}

// Run executes one or more validations and returns the first error encountered.
// obj must not be modified. If it is, this indicates a programming error and the method will panic.
func (r *SingleRunner[O]) Run(ctx context.Context, obj O, validations ...Validation[O]) error {
	copyObj := obj.DeepCopy()
	for _, validation := range validations {
		if !r.shouldRun(validation.Name) {
			continue
		}

		err := validation.Validate(ctx, r.informer, copyObj)
		if err != nil {
			return err
		}
	}

	if !reflect.DeepEqual(obj, copyObj) {
		panic("validations must not modify the object under validation")
	}

	return nil
}

func (r *SingleRunner[O]) shouldRun(name string) bool {
	return !slices.Contains(r.config.skipValidations, name)
}
