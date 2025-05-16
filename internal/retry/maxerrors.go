package retry

import "fmt"

func NewMaxConsecutiveErrorHandler(maxAttempts int) HandleError {
	attempts := 0
	return func(err error) error {
		if err == nil {
			attempts = 0
		} else {
			attempts++
		}

		if attempts <= maxAttempts {
			return nil
		}
		return fmt.Errorf("max attempts %d reached: %w", maxAttempts, err)
	}
}
