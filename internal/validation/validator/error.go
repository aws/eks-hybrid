package validator

import "strings"

type FailError struct {
	err string
}

func (e *FailError) Error() string {
	if e.err == "" {
		e.err = "validation failed"
	}
	return e.err
}

type WarningError struct {
	err string
}

func (e *WarningError) Error() string {
	if e.err == "" {
		e.err = "validation warning"
	}
	return e.err
}

func FormatErrors(errors []error) string {
	var sb strings.Builder

	for i, err := range errors {
		if i == 0 {
			sb.WriteString(err.Error())
		} else {
			sb.WriteString(", ")
			sb.WriteString(err.Error())
		}
	}

	return sb.String()
}
