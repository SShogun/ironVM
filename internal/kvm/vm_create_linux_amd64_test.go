//go:build linux && amd64

package kvm

import (
	"errors"
	"strings"
	"syscall"
	"testing"
)

func TestCreateVMFDRetriesEINTR(t *testing.T) {
	attempts := 0
	ioctl := func(trap, fd, request, arg uintptr) (uintptr, uintptr, syscall.Errno) {
		attempts++
		if trap != syscall.SYS_IOCTL || fd != 17 || request != 0xAE01 || arg != 0 {
			t.Fatalf("ioctl arguments = (%d, %d, %#x, %d), want (SYS_IOCTL, 17, 0xAE01, 0)", trap, fd, request, arg)
		}
		if attempts < 3 {
			return ^uintptr(0), 0, syscall.EINTR
		}
		if attempts > 3 {
			t.Fatal("retried successful KVM_CREATE_VM")
		}
		return 23, 0, 0
	}

	fd, err := createVMFD(17, ioctl)
	if err != nil {
		t.Fatalf("createVMFD after EINTR = %v, want successful retry", err)
	}
	if fd != 23 || attempts != 3 {
		t.Fatalf("createVMFD = fd %d after %d attempts, want fd 23 after 3 attempts", fd, attempts)
	}
}

func TestCreateVMFDReturnsOtherErrnos(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EINVAL, syscall.EPERM, syscall.ENOMEM, syscall.EBUSY} {
		for _, interrupted := range []bool{false, true} {
			name := errno.Error() + "/direct"
			if interrupted {
				name = errno.Error() + "/after EINTR"
			}
			t.Run(name, func(t *testing.T) {
				attempts := 0
				wantAttempts := 1
				if interrupted {
					wantAttempts = 2
				}
				ioctl := func(trap, fd, request, arg uintptr) (uintptr, uintptr, syscall.Errno) {
					attempts++
					if attempts > wantAttempts {
						t.Fatal("retried non-EINTR errno")
					}
					if interrupted && attempts == 1 {
						return ^uintptr(0), 0, syscall.EINTR
					}
					return ^uintptr(0), 0, errno
				}

				_, err := createVMFD(17, ioctl)
				if !errors.Is(err, errno) || !strings.Contains(err.Error(), "ioctl KVM_CREATE_VM") {
					t.Fatalf("createVMFD error = %v, want contextual %v", err, errno)
				}
				if attempts != wantAttempts {
					t.Fatalf("attempts = %d, want %d", attempts, wantAttempts)
				}
			})
		}
	}
}
