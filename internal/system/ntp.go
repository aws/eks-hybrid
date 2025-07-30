package system

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/validation"
)

var localhostReferenceIDs = []string{
	"(00000000)",
	"(0.0.0.0)",
	"(127.0.0.1)",
}

// NTPValidator validates NTP synchronization status
type NTPValidator struct{}

type baseError struct {
	message string
	cause   error
}

func (e *baseError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.message, e.cause)
	}
	return e.message
}

func (e *baseError) Unwrap() error {
	return e.cause
}

type ChronycSynchronizationError struct {
	baseError
}

type TimedatectlSynchronizationError struct {
	baseError
}

// NewNTPValidator creates a new NTP validator
func NewNTPValidator() *NTPValidator {
	return &NTPValidator{}
}

// Run validates NTP synchronization
func (v *NTPValidator) Run(ctx context.Context, informer validation.Informer, _ *api.NodeConfig) error {
	var err error
	informer.Starting(ctx, "ntp-sync", "Validating NTP synchronization status")
	defer func() {
		informer.Done(ctx, "ntp-sync", err)
	}()
	if err = v.Validate(); err != nil {
		err = addNTPRemediation(err)
		return err
	}

	return nil
}

// Validate performs the actual NTP validation
func (v *NTPValidator) Validate() error {
	if v.commandExists("chronyc") {
		if commandFailed, err := v.checkChronyc(); err != nil {
			if commandFailed {
				return err
			}
			return &ChronycSynchronizationError{baseError{message: "validating NTP synchronization via chronyc", cause: err}}
		}
	}

	if v.commandExists("timedatectl") {
		if commandFailed, err := v.checkTimedatectl(); err != nil {
			if commandFailed {
				return err
			}
			return &TimedatectlSynchronizationError{baseError{message: "validating NTP synchronization via timedatectl", cause: err}}
		}
	}

	return nil
}

// checkChronyd uses chronyc to check if system clock is synchronized
func (v *NTPValidator) checkChronyc() (bool, error) {
	hasReference := false
	leapStatusNormal := false

	cmd := exec.Command("chronyc", "tracking")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return true, fmt.Errorf("getting system clock settings from chronyc: %s, error: %w", strings.TrimSpace(string(output)), err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Reference ID") {
			parts := strings.Fields(line)
			if len(parts) >= 5 && !slices.Contains(localhostReferenceIDs, parts[4]) {
				hasReference = true
			}
		}

		if strings.Contains(line, "Leap status") && strings.Contains(line, "Normal") {
			leapStatusNormal = true
		}
	}

	if !hasReference || !leapStatusNormal {
		return false, fmt.Errorf("chronyd not synchronized")
	}

	return false, nil
}

// checkTimedatectl uses timedatectl to check if system clock is synchronized
func (v *NTPValidator) checkTimedatectl() (bool, error) {
	ntpSynchronized := false

	cmd := exec.Command("timedatectl", "status")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return true, fmt.Errorf("getting system clock settings from timedatectl: %s, error: %w", strings.TrimSpace(string(output)), err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "System clock synchronized: yes") {
			ntpSynchronized = true
		}
	}

	if !ntpSynchronized {
		return false, fmt.Errorf("System clock not synchronized")
	}

	return false, nil
}

func (v *NTPValidator) commandExists(command string) bool {
	_, err := exec.LookPath(command)
	return err == nil
}

func addNTPRemediation(err error) error {
	errWithContext := fmt.Errorf("validating NTP synchronization: %w", err)

	switch err.(type) {
	case *ChronycSynchronizationError:
		return validation.WithRemediation(err,
			"Ensure the hybrid node is synchronized with NTP through chronyd services. "+
				"Verify NTP server configuration in /etc/chrony.conf. "+
				"If using airgapped networks, ensure chrony is configured with local NTP sources and adjusted manually.",
		)
	case *TimedatectlSynchronizationError:
		return validation.WithRemediation(err,
			"Ensure the hybrid node is synchronized with NTP by running `timedatectl set-ntp true`.",
		)
	}
	return errWithContext
}
