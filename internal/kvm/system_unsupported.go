//go:build !linux || !amd64

package kvm

import "errors"

// OpenSystem reports that KVM is supported only on Linux/amd64.
func OpenSystem() (*System, error) {
	return nil, &EnvironmentError{
		operation: "open /dev/kvm",
		err:       errors.New("IronVM requires a Linux/amd64 host"),
	}
}

// APIVersion returns the version reported by KVM_GET_API_VERSION.
func (s *System) APIVersion() int {
	return s.apiVersion
}

// Close releases the /dev/kvm file descriptor.
func (s *System) Close() error {
	if s.closed {
		return errSystemClosed
	}
	s.closed = true
	return nil
}
