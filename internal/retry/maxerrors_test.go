package retry_test

import (
	"errors"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/retry"
)

// errorHandlerTestStep defines a single step in a NewMaxConsecutiveErrorHandler test sequence.
type errorHandlerTestStep struct {
	inputError    error
	expectedError string // empty if nil, otherwise substring to match
}

func TestNewMaxConsecutiveErrorHandler(t *testing.T) {
	testErrBase := errors.New("test error for handler")

	tests := []struct {
		name        string
		maxAttempts int
		sequence    []errorHandlerTestStep
	}{
		{
			name:        "maxAttempts = 0, first error exceeds",
			maxAttempts: 0,
			sequence: []errorHandlerTestStep{
				{inputError: testErrBase, expectedError: fmt.Sprintf("max attempts 0 reached: %s", testErrBase.Error())},
				{inputError: nil, expectedError: ""},
				{inputError: testErrBase, expectedError: fmt.Sprintf("max attempts 0 reached: %s", testErrBase.Error())}, // Counter resets, but still 0 attempts allowed
			},
		},
		{
			name:        "maxAttempts = 1",
			maxAttempts: 1,
			sequence: []errorHandlerTestStep{
				{inputError: testErrBase, expectedError: ""},                                                             // 1st error, attempts = 1, <= maxAttempts
				{inputError: testErrBase, expectedError: fmt.Sprintf("max attempts 1 reached: %s", testErrBase.Error())}, // 2nd error, attempts = 2, > maxAttempts
				{inputError: nil, expectedError: ""},                                                                     // Reset attempts
				{inputError: testErrBase, expectedError: ""},                                                             // 1st error again
				{inputError: errors.New("another error"), expectedError: "max attempts 1 reached: another error"},        // 2nd error
			},
		},
		{
			name:        "maxAttempts = 2",
			maxAttempts: 2,
			sequence: []errorHandlerTestStep{
				{inputError: testErrBase, expectedError: ""},                                                             // 1st error
				{inputError: testErrBase, expectedError: ""},                                                             // 2nd error
				{inputError: testErrBase, expectedError: fmt.Sprintf("max attempts 2 reached: %s", testErrBase.Error())}, // 3rd error
				{inputError: nil, expectedError: ""},                                                                     // Reset
				{inputError: testErrBase, expectedError: ""},                                                             // 1st again
				{inputError: testErrBase, expectedError: ""},                                                             // 2nd again
				{inputError: testErrBase, expectedError: fmt.Sprintf("max attempts 2 reached: %s", testErrBase.Error())}, // 3rd again
			},
		},
		{
			name:        "nil inputs do not increment attempts and reset count",
			maxAttempts: 1,
			sequence: []errorHandlerTestStep{
				{inputError: nil, expectedError: ""},
				{inputError: testErrBase, expectedError: ""}, // 1st error
				{inputError: nil, expectedError: ""},         // Reset
				{inputError: nil, expectedError: ""},
				{inputError: testErrBase, expectedError: ""}, // 1st error again
				{inputError: testErrBase, expectedError: fmt.Sprintf("max attempts 1 reached: %s", testErrBase.Error())}, // 2nd error
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			handler := retry.NewMaxConsecutiveErrorHandler(tt.maxAttempts)
			for i, step := range tt.sequence {
				returnedErr := handler(step.inputError)
				if step.expectedError == "" {
					g.Expect(returnedErr).ToNot(HaveOccurred(), "Test: %s, Step: %d", tt.name, i+1)
				} else {
					g.Expect(returnedErr).To(MatchError(ContainSubstring(step.expectedError)), "Test: %s, Step: %d", tt.name, i+1)
					if step.inputError != nil {
						g.Expect(errors.Is(returnedErr, step.inputError)).To(BeTrue(), "Test: %s, Step: %d, error should wrap input error", tt.name, i+1)
					}
				}
			}
		})
	}
}
