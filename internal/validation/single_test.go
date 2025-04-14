package validation_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/validation"
)

func TestSingleRunnerRunSuccess(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	r := validation.NewSingleRunner[*nodeConfig](validation.NewPrinter())

	config := &nodeConfig{
		maxPods: 3,
		name:    "my-node-1",
	}

	err := r.Run(ctx, config,
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
	)

	g.Expect(err).To(Succeed())
}

func TestSingleRunnerRunError(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	r := validation.NewSingleRunner[*nodeConfig](validation.NewPrinter())

	e1 := errors.New("name can't be empty")
	e2 := errors.New("maxPods can't be 0")

	config := &nodeConfig{
		maxPods: 0,
		name:    "",
	}

	err := r.Run(ctx, config,
		newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			if config.name == "" {
				return e1
			}
			return nil
		}),
		newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
			if config.maxPods == 0 {
				return e2
			}
			return nil
		}),
	)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err).To(MatchError(e1))
}

func TestSingleRunnerRunPanicAfterModifyingObject(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	r := validation.NewSingleRunner[*nodeConfig](validation.NewPrinter())

	config := &nodeConfig{}
	run := func() {
		_ = r.Run(ctx, config,
			newValidation(func(ctx context.Context, _ validation.Informer, config *nodeConfig) error {
				config.maxPods = 5
				return nil
			}),
		)
	}
	g.Expect(run).To(PanicWith("validations must not modify the object under validation"))
}

func TestSingleRunnerWithSkipValidations(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	r := validation.NewSingleRunner[*nodeConfig](
		validation.NewPrinter(),
		validation.WithSingleRunnerSkipValidations("my-validation-1", "my-validation-2"),
	)

	config := &nodeConfig{
		maxPods: 3,
		name:    "my-node-1",
	}

	err := r.Run(ctx, config,
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
	)

	g.Expect(err).To(Succeed())
}
