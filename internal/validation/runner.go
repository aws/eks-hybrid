package validation

import (
	"context"
	"errors"
	"reflect"
	"strings"
)

// Validatable is anything that can be validated.
type Validatable[O any] interface {
	DeepCopy() O
}

// ValidationRunner is an interface for running validations.
type ValidationRunner[O Validatable[O]] interface {
	Run(ctx context.Context, obj O, validations ...Validation[O]) error
}

// Validation validates the system for a type O.
type Validation[O Validatable[O]] struct {
	Name     string
	Validate Validate[O]
}

func New[O Validatable[O]](name string, validate Validate[O]) Validation[O] {
	return Validation[O]{Name: name, Validate: validate}
}

// Validate is the logic for a validation of a type O.
type Validate[O Validatable[O]] func(ctx context.Context, informer Informer, obj O) error

// Runner allows to compose and run validations.
type Runner[O Validatable[O]] struct {
	validations []Validation[O]
	informer    Informer
	config      RunnerConfig
}

type Informer interface {
	Starting(ctx context.Context, name, message string)
	Done(ctx context.Context, name string, err error)
}

// RunnerConfig holds the configuration for the Runner.
type RunnerConfig struct {
	skipValidations []string
}

// RunnerOpt allows to configure the Runner.
type RunnerOpt func(*RunnerConfig)

// WithSkipValidation configures the runner to skip
// the validations with the given names.
func WithSkipValidations(namesToSkip ...string) RunnerOpt {
	return func(c *RunnerConfig) {
		c.skipValidations = append(c.skipValidations, namesToSkip...)
	}
}

// NewRunner constructs a new Runner.
func NewRunner[O Validatable[O]](informer Informer, opts ...RunnerOpt) *Runner[O] {
	r := &Runner[O]{
		informer: informer,
	}

	for _, opt := range opts {
		opt(&r.config)
	}

	return r
}

// Register adds validations to the Runner.
func (r *Runner[O]) Register(validations ...Validation[O]) {
	for _, v := range validations {
		if r.shouldRegister(v.Name) {
			r.validations = append(r.validations, v)
		}
	}
}

// Sequentially runs all validations one after the other and waits until they all finish,
// aggregating the errors if present. obj must not be modified. If it is, this
// indicates a programming error and the method will panic.
func (r *Runner[O]) Sequentially(ctx context.Context, obj O) error {
	copyObj := obj.DeepCopy()
	var errs []error

	for _, validation := range r.validations {
		err := validation.Validate(ctx, r.informer, copyObj)
		if err != nil {
			errs = append(errs, Unwrap(err)...)
		}
	}

	if !reflect.DeepEqual(obj, copyObj) {
		panic("validations must not modify the object under validation")
	}

	return errors.Join(errs...)
}

func (r *Runner[O]) UntilError(validations ...Validation[O]) Validation[O] {
	var accepted []Validate[O]
	var names []string
	for _, v := range validations {
		if r.shouldRegister(v.Name) {
			accepted = append(accepted, v.Validate)
			names = append(names, v.Name)
		}
	}

	return New("until-error-"+strings.Join(names, "/"), UntilError(accepted...))
}

func (r *Runner[O]) shouldRegister(name string) bool {
	for _, skip := range r.config.skipValidations {
		if skip == name {
			return false
		}
	}

	return true
}

// Unwrap unfolds and flattens errors if err implements Unwrap []error.
// If it doesn't implement it, it just returns a slice with one single error.
func Unwrap(err error) []error {
	if agg, ok := err.(interface{ Unwrap() []error }); ok {
		return agg.Unwrap()
	}

	return []error{err}
}

// UntilError returns a composed validate that runs all validations until one fails.
func UntilError[O Validatable[O]](validates ...Validate[O]) Validate[O] {
	return func(ctx context.Context, informer Informer, obj O) error {
		for _, v := range validates {
			if err := v(ctx, informer, obj); err != nil {
				return err
			}
		}

		return nil
	}
}
