package retrier

import "time"

// Constants used for validation operations across packages
const (
	// ValidationInterval is the interval between validation attempts
	ValidationInterval = 10 * time.Second

	// ValidationTimeout is the maximum time to wait for validation to complete
	ValidationTimeout = 1 * time.Minute

	// ValidationMaxRetries is the maximum number of consecutive errors before failing
	ValidationMaxRetries = 5
)
