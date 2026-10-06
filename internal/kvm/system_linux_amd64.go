package kvm

import (
	"errors"
	"fmt"
	"syscall"
)

const (
	kvmDevice        = "/dev/kvm"
	kvmGetAPIVersion = 0xAE00
)

// OpenSystem opens /dev/kvm and verifies that the host supports KVM API version 12.
func OpenSystem() (*System, error) {
	fd, err := syscall.Open(kvmDevice, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, &EnvironmentError{operation: "open /dev/kvm", err: err}
	}

	version, _, ioctlErrno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), kvmGetAPIVersion, 0)
	if ioctlErrno != 0 {
		err := &EnvironmentError{
			operation: "query KVM API version",
			err:       fmt.Errorf("ioctl KVM_GET_API_VERSION: %w", ioctlErrno),
		}
		return nil, closeAfterFailure(fd, err)
	}
	if version != apiVersion {
		err := fmt.Errorf("KVM API version mismatch: got %d, want %d", version, apiVersion)
		return nil, closeAfterFailure(fd, err)
	}

	return &System{fd: fd, apiVersion: int(version)}, nil
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
	if err := syscall.Close(s.fd); err != nil {
		return fmt.Errorf("close /dev/kvm: %w", err)
	}
	return nil
}

func closeAfterFailure(fd int, cause error) error {
	if err := syscall.Close(fd); err != nil {
		return errors.Join(cause, fmt.Errorf("close /dev/kvm: %w", err))
	}
	return cause
}
