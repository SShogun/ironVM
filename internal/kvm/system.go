package kvm

import (
	"errors"
	"fmt"
)

const apiVersion = 12

var errSystemClosed = errors.New("KVM system is already closed")

// System owns the file descriptor for /dev/kvm.
type System struct {
	fd         int
	apiVersion int
	closed     bool
}

// EnvironmentError reports that the host cannot provide a usable KVM system fd.
type EnvironmentError struct {
	operation string
	err       error
}

func (e *EnvironmentError) Error() string {
	return fmt.Sprintf("%s: %v", e.operation, e.err)
}

func (e *EnvironmentError) Unwrap() error {
	return e.err
}
