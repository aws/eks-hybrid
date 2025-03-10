package suite

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
)

const retryableHeading = "Retryable FlakyCode failure"

// FlakyCode can be used to run a test block multiple times until it succeeds.
// It will retry the test block the number of times specified by flakeAttempts.
// If the test block fails, the deferred cleanups will be run.
// If the test block succeeds, the deferred cleanups will not be run until the end of the entire test.
type FlakyCode struct {
	Logger logr.Logger
}
type FlakeRun struct {
	DeferCleanup    func(func(context.Context), ...interface{})
	RetryableExpect func(actual interface{}, extra ...interface{}) Assertion
}

type retryable struct {
	panicableError error
	retryableError error
	testGoMega     gomega.Gomega
}

func newRetryable(attempt, maxAttempts int) *retryable {
	return &retryable{
		testGoMega: gomega.NewGomega(func(message string, callerSkip ...int) {
			skip := 0
			if len(callerSkip) > 0 {
				skip = callerSkip[0]
			}

			// not calling Fail when we are going to retry because Fail stores the error
			// and if a future attempt succeeds, ginkgo will still fail the test with the original error
			if attempt < maxAttempts-1 {
				// we add 1 to skip to remove this callback from the stacktrace
				cl := types.NewCodeLocationWithStackTrace(skip + 1)
				panic(types.GinkgoError{
					Heading:      retryableHeading,
					Message:      message,
					CodeLocation: cl,
				})
			}

			// if we arent retrying, we call Fail directly which triggers
			// ginkgo to store the error and stacktrace as a normal test failure
			Fail(message, skip+1)
		}),
	}
}

func (r *retryable) expect(actual interface{}, extra ...interface{}) Assertion {
	return r.testGoMega.Expect(actual, extra...)
}

func (r *retryable) recover() {
	e := recover()

	// if there is no panic, we don't need to store the error
	// and the test can continue
	if e == nil {
		return
	}

	err, ok := e.(types.GinkgoError)
	if !ok {
		r.panicableError = fmt.Errorf("unknown panic: %v", e)
		return
	}

	// if not retrying, store the error and return to fail the test
	if err.Heading != retryableHeading {
		r.panicableError = err
		return
	}

	r.retryableError = err
}

func (f *FlakyCode) It(ctx context.Context, description string, flakeAttempts int, body func(ctx context.Context, flakeRun FlakeRun)) {
	for attempt := range flakeAttempts {
		// register globally as well in case the test fails for any reason
		// including being cancelled while this block is executing
		// track if the cleanup runs and do not run it again if it does
		cleanups := []func(context.Context){}
		deferCleanup := func(cleanup func(context.Context), args ...interface{}) {
			ran := false
			onced := func(ctx context.Context) {
				if ran {
					f.Logger.Info(fmt.Sprintf("Cleanup already ran for flake attempt %d, skipping", attempt+1))
					return
				}
				ran = true
				cleanup(ctx)
			}
			cleanups = append(cleanups, onced)
			DeferCleanup(onced, args)
		}

		retry := newRetryable(attempt, flakeAttempts)

		flakeRun := FlakeRun{
			DeferCleanup:    deferCleanup,
			RetryableExpect: retry.expect,
		}

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer retry.recover()

			By(fmt.Sprintf("%s (attempt %d/%d)", description, attempt+1, flakeAttempts))
			body(ctx, flakeRun)
		}()

		wg.Wait()

		// we need to "re-panic" the error to trigger the main ginkgo go routine for this test to fail
		// this error will have already been caught in ginkgo's Fail handler and stored on the test
		// so when we expect here, the stacktrace and error message are pulled from what was stored via the Fail handler
		// which is called by Expect and RetryableExpect when its the last attempt
		Expect(retry.panicableError).NotTo(HaveOccurred())

		if retry.retryableError == nil {
			if attempt > 0 {
				f.Logger.Info(fmt.Sprintf("Succeeded on attempt %d after previous failures", attempt+1))
			}
			return
		}

		// run cleans before next attempt
		for _, f := range slices.Backward(cleanups) {
			f(ctx)
		}

		f.Logger.Info(fmt.Sprintf("Failed attempt %d/%d", attempt+1, flakeAttempts))
		f.Logger.Error(nil, retry.retryableError.Error())
		time.Sleep(time.Second * time.Duration(attempt))

	}
}
