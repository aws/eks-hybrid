package suite

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
)

type FlakeAttempt struct {
	logger logr.Logger
}
type FlakeRun struct {
	DeferCleanup    func(func(context.Context), ...interface{})
	RetryableExpect func(actual interface{}, extra ...interface{}) Assertion
}

type IgnorablePanic struct{}

func (IgnorablePanic) GinkgoRecoverShouldIgnoreThisPanic() {}

func (f *FlakeAttempt) It(ctx context.Context, description string, flakeAttempts int, body func(ctx context.Context, flakeRun FlakeRun)) {
	var lastErr error
	for attempt := range flakeAttempts {
		// register globally as well in case the test fails for any reason
		// including being cancelled while this block is executing
		// track if the cleanup runs and do not run it again if it does
		cleanups := []func(context.Context){}
		deferCleanup := func(cleanup func(context.Context), args ...interface{}) {
			ran := false
			onced := func(ctx context.Context) {
				if ran {
					f.logger.Info(fmt.Sprintf("Cleanup already ran for flake attempt %d, skipping", attempt+1))
					return
				}
				ran = true
				cleanup(ctx)
			}
			cleanups = append(cleanups, onced)
			DeferCleanup(onced, args)
		}

		testGoMega := gomega.NewGomega(func(message string, callerSkip ...int) {
			lastErr = fmt.Errorf("%s", message)
			panic(IgnorablePanic{})
		})
		flakeRun := FlakeRun{
			DeferCleanup:    deferCleanup,
			RetryableExpect: testGoMega.Expect,
		}

		c := make(chan error)
		go func() {
			defer func() {
				var err error
				// if this was a ignorable panic, we send nil
				// to indicate that we should retry
				// otherwise we send the error to end execution
				e := recover()
				if lastErr == nil && e != nil {
					err = e.(error)
				}
				c <- err
			}()
			By(description)
			body(ctx, flakeRun)
		}()

		var panicableError error
		Eventually(c).WithContext(ctx).Should(Receive(&panicableError))
		Expect(panicableError).NotTo(HaveOccurred())

		if lastErr == nil {
			if attempt > 1 {
				f.logger.Info(fmt.Sprintf("Succeeded on attempt %d after previous failures", attempt+1))
			}
			break
		}

		if attempt == flakeAttempts-1 {
			break
		}

		// omly run cleanups if about to retry, otherwise let gingko handle it
		for _, f := range slices.Backward(cleanups) {
			f(ctx)
		}

		f.logger.Error(lastErr, fmt.Sprintf("Failed attempt %d/%d", attempt+1, flakeAttempts))
		time.Sleep(time.Second * time.Duration(attempt))

	}
	Expect(lastErr).To(BeNil(), "should have succeeded")
}
