package validation_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/validation"
)

func TestRunnerRunAllSuccess(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	r := validation.NewRunner[*nodeConfig](validation.NewPrinter())
	r.Register(
		newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			if config.maxPods == 0 {
				return errors.New("maxPods can't be 0")
			}

			return nil
		}),
		newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			if config.name == "" {
				return errors.New("name can't be empty")
			}

			return nil
		}),
		r.UntilError(
			newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
				return nil
			}),
			newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
				return nil
			}),
		),
	)

	config := &nodeConfig{
		maxPods: 3,
		name:    "my-node-1",
	}

	g.Expect(r.Sequentially(ctx, config)).To(Succeed())
}

func TestRunnerRunAllError(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	r := validation.NewRunner[*nodeConfig](validation.NewPrinter())

	e1 := errors.New("name can't be empty")
	e2 := errors.New("invalid 1")
	e3 := validation.NewRemediableErr("invalid 2", "fix this and that")
	e4 := errors.New("invalid 3")
	e5 := errors.New("invalid 4")
	r.Register(
		newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			if config.name == "" {
				return e1
			}

			return nil
		}),
		newValidation(func(ctx context.Context, _ validation.Informer, _ *nodeConfig) error {
			var errs []error

			errs = append(errs, e2)
			errs = append(errs, e3)

			return errors.Join(errs...)
		}),
		r.UntilError(
			newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
				return e4
			}),
			newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
				return e5
			}),
		),
	)

	config := &nodeConfig{
		maxPods: 0,
		name:    "",
	}
	err := r.Sequentially(ctx, config)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err).To(MatchError(ContainSubstring("name can't be empty")))
	g.Expect(err).To(MatchError(ContainSubstring("invalid 1")))
	g.Expect(err).To(MatchError(ContainSubstring("invalid 2")))
	g.Expect(validation.Unwrap(err)).To(ConsistOf(e1, e2, e3, e4))
	g.Expect(err).NotTo(MatchError(ContainSubstring("invalid 4")))
}

func TestRunnerRunAllPanicAfterModifyingObject(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	r := validation.NewRunner[*nodeConfig](validation.NewPrinter())
	r.Register(

		newValidation(func(ctx context.Context, _ validation.Informer, _ *nodeConfig) error {
			return nil
		}),
		newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			config.maxPods = 5
			return nil
		}),
	)

	config := &nodeConfig{}
	run := func() {
		_ = r.Sequentially(ctx, config)
	}
	g.Expect(run).To(PanicWith("validations must not modify the object under validation"))
}

func TestRunnerWithSkipValidations(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	r := validation.NewRunner[*nodeConfig](
		validation.NewPrinter(),
		validation.WithSkipValidations("my-validation-1", "my-validation-2"),
	)

	r.Register(
		newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			if config.maxPods == 0 {
				return errors.New("maxPods can't be 0")
			}

			return nil
		}),
		validation.New("my-validation-1", func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			return errors.New("this should be skipped")
		}),
		newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			if config.name == "" {
				return errors.New("name can't be empty")
			}

			return nil
		}),
		validation.New("my-validation-2", func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			return errors.New("this should be skipped as well")
		}),
		r.UntilError(
			newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
				return nil
			}),
			newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
				return nil
			}),
		),
	)

	config := &nodeConfig{
		maxPods: 3,
		name:    "my-node-1",
	}

	g.Expect(r.Sequentially(ctx, config)).To(Succeed())
}

type nodeConfig struct {
	maxPods int
	name    string
}

func (a *nodeConfig) DeepCopy() *nodeConfig {
	copy := *a
	return &copy
}

func newValidation(run func(ctx context.Context, informer validation.Informer, obj *nodeConfig) error) validation.Validation[*nodeConfig] {
	return validation.Validation[*nodeConfig]{
		Name:     "my-validation",
		Validate: run,
	}
}
