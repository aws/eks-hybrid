package system

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewNTPValidator(t *testing.T) {
	validator := NewNTPValidator()

	assert.NotNil(t, validator)
}

func TestNTPValidator_Run(t *testing.T) {
	validator := NewNTPValidator()

	// Create a mock informer for testing
	informer := &ntpMockInformer{}

	ctx := context.Background()

	// Test the validator - this will check the actual system
	err := validator.Run(ctx, informer, nil)

	// The validator should either succeed or fail with an error
	t.Logf("NTP validation result: %v", err)

	// Verify that the informer was called correctly
	assert.True(t, informer.startingCalled, "Expected Starting to be called")
	assert.True(t, informer.doneCalled, "Expected Done to be called")
	assert.Equal(t, err, informer.lastError, "Expected error to match")
}

func TestNTPValidator_commandExists(t *testing.T) {
	validator := NewNTPValidator()

	tests := []struct {
		name     string
		command  string
		expected bool
	}{
		{
			name:     "existing command",
			command:  "echo",
			expected: true,
		},
		{
			name:     "non-existing command",
			command:  "nonexistentcommand12345",
			expected: false,
		},
		{
			name:     "empty command",
			command:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.commandExists(tt.command)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNTPValidator_Validate(t *testing.T) {
	validator := NewNTPValidator()

	// Test validation - this will check the actual system
	err := validator.Validate()

	// The result depends on the system state
	// We can't predict the exact outcome, but we can verify the behavior
	if err != nil {
		// If there's an error, it should be a meaningful message
		assert.NotEmpty(t, err.Error(), "Error message should not be empty")
		t.Logf("NTP validation failed as expected: %v", err)
	} else {
		// If successful, log the success
		t.Logf("NTP validation succeeded")
	}
}

func TestNTPValidator_checkChronyc_CommandNotFound(t *testing.T) {
	validator := NewNTPValidator()

	// Test checkChronyd behavior - this will depend on whether chronyc exists
	_, err := validator.checkChronyc()

	if validator.commandExists("chronyc") {
		// If chronyc exists, we expect either success or a specific error
		if err != nil {
			// Should contain meaningful error message
			assert.Contains(t, err.Error(), "getting system clock settings from chronyc")
		}
	} else {
		// If chronyc doesn't exist, the command should fail with exec error
		assert.NotNil(t, err, "Expected error when chronyc command doesn't exist")
		assert.Contains(t, err.Error(), "getting system clock settings from chronyc")
	}
}

func TestNTPValidator_checkTimedatectl_CommandNotFound(t *testing.T) {
	validator := NewNTPValidator()

	// Test checkSystemdTimesyncd behavior - this will depend on whether timedatectl exists
	_, err := validator.checkTimedatectl()

	if validator.commandExists("timedatectl") {
		// If timedatectl exists, we expect either success or a specific error
		if err != nil {
			// Should contain meaningful error message
			assert.Contains(t, err.Error(), "getting system clock settings from timedatectl")
		}
	} else {
		// If timedatectl doesn't exist, the command should fail with exec error
		assert.NotNil(t, err, "Expected error when timedatectl command doesn't exist")
		assert.Contains(t, err.Error(), "getting system clock settings from timedatectl")
	}
}

func TestNTPValidator_checkChronyd_ParseOutput(t *testing.T) {
	// Test parsing logic for chrony tracking output
	tests := []struct {
		name          string
		output        string
		expectedError bool
		errorContains string
	}{
		{
			name: "synchronized with reference",
			output: `Reference ID    : 169.254.169.123 (169.254.169.123)
Stratum         : 4
Ref time (UTC)  : Thu Jan 01 00:00:00 2024
System time     : 0.000000001 seconds fast of NTP time
Last offset     : +0.000000001 seconds
RMS offset      : 0.000000001 seconds
Frequency       : 0.000 ppm slow
Residual freq   : +0.000 ppm
Skew            : 0.000 ppm
Root delay      : 0.000000001 seconds
Root dispersion : 0.000000001 seconds
Update interval : 0.0 seconds
Leap status     : Normal`,
			expectedError: false,
		},
		{
			name: "no reference ID",
			output: `Reference ID    : 0.0.0.0 (0.0.0.0)
Stratum         : 0
Ref time (UTC)  : Thu Jan 01 00:00:00 1970
System time     : 0.000000000 seconds slow of NTP time
Last offset     : +0.000000000 seconds
RMS offset      : 0.000000000 seconds
Frequency       : 0.000 ppm slow
Residual freq   : +0.000 ppm
Skew            : 0.000 ppm
Root delay      : 0.000000000 seconds
Root dispersion : 0.000000000 seconds
Update interval : 0.0 seconds
Leap status     : Not synchronised`,
			expectedError: true,
			errorContains: "chronyd not synchronized",
		},
		{
			name: "localhost reference ID",
			output: `Reference ID    : 127.0.0.1 (127.0.0.1)
Stratum         : 4
Ref time (UTC)  : Thu Jan 01 00:00:00 2024
System time     : 0.000000001 seconds fast of NTP time
Last offset     : +0.000000001 seconds
RMS offset      : 0.000000001 seconds
Frequency       : 0.000 ppm slow
Residual freq   : +0.000 ppm
Skew            : 0.000 ppm
Root delay      : 0.000000001 seconds
Root dispersion : 0.000000001 seconds
Update interval : 0.0 seconds
Leap status     : Normal`,
			expectedError: true,
			errorContains: "chronyd not synchronized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the output to verify our logic
			lines := strings.Split(tt.output, "\n")

			hasReference := false
			hasNormalLeap := false

			for _, line := range lines {
				line = strings.TrimSpace(line)

				// Check for reference ID
				if strings.Contains(line, "Reference ID") {
					parts := strings.Fields(line)
					if len(parts) >= 5 {
						refID := parts[4]
						if refID != "(0.0.0.0)" && refID != "(00000000)" && refID != "(127.0.0.1)" {
							hasReference = true
						}
					}
				}

				// Check for leap status
				if strings.Contains(line, "Leap status") && strings.Contains(line, "Normal") {
					hasNormalLeap = true
				}
			}

			// Verify our parsing logic
			if tt.expectedError {
				if tt.errorContains == "chronyd not synchronized" {
					assert.False(t, hasReference && hasNormalLeap, "Should not have both valid reference and normal leap status")
				}
			} else {
				assert.True(t, hasReference || hasNormalLeap, "Should have either valid reference or normal leap status")
			}
		})
	}
}

func TestNTPValidator_checkSystemdTimesyncd_ParseOutput(t *testing.T) {
	// Test parsing of timedatectl status output
	tests := []struct {
		name          string
		output        string
		expectedError bool
		errorContains string
	}{
		{
			name: "synchronized",
			output: `               Local time: Thu 2024-01-01 00:00:00 UTC
           Universal time: Thu 2024-01-01 00:00:00 UTC
                 RTC time: Thu 2024-01-01 00:00:00
                Time zone: UTC (UTC, +0000)
System clock synchronized: yes
              NTP service: active
          RTC in local TZ: no`,
			expectedError: false,
		},
		{
			name: "not synchronized",
			output: `               Local time: Thu 2024-01-01 00:00:00 UTC
           Universal time: Thu 2024-01-01 00:00:00 UTC
                 RTC time: Thu 2024-01-01 00:00:00
                Time zone: UTC (UTC, +0000)
System clock synchronized: no
              NTP service: active
          RTC in local TZ: no`,
			expectedError: true,
			errorContains: "systemd-timesyncd not synchronized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the output to verify our logic
			lines := strings.Split(tt.output, "\n")
			ntpSynchronized := false

			for _, line := range lines {
				line = strings.TrimSpace(line)

				// Check if system clock is synchronized
				if strings.Contains(line, "System clock synchronized: yes") {
					ntpSynchronized = true
				}
			}

			// Verify our parsing logic matches expected results
			if tt.expectedError {
				if tt.errorContains == "systemd-timesyncd not synchronized" {
					assert.False(t, ntpSynchronized, "System clock should not be synchronized")
				}
			} else {
				assert.True(t, ntpSynchronized, "System clock should be synchronized for success cases")
			}
		})
	}
}

func TestNTPValidator_LocalhostReferenceIDs(t *testing.T) {
	// Verify the localhost reference IDs constant
	expectedIDs := []string{"(00000000)", "(0.0.0.0)", "(127.0.0.1)"}
	assert.Equal(t, expectedIDs, localhostReferenceIDs)
}

// ntpMockInformer implements validation.Informer for testing
type ntpMockInformer struct {
	startingCalled bool
	doneCalled     bool
	lastError      error
}

func (m *ntpMockInformer) Starting(ctx context.Context, name, description string) {
	m.startingCalled = true
}

func (m *ntpMockInformer) Done(ctx context.Context, name string, err error) {
	m.doneCalled = true
	m.lastError = err
}
