//go:build linux && amd64

package kvm

import (
	"errors"
	"syscall"
	"testing"
)

func TestGuestHLTExitsWithKVMExitHLT(t *testing.T) {
	system, err := OpenSystem()
	if err != nil {
		if isKVMUnavailable(err) {
			t.Skipf("KVM integration environment unavailable: %v", err)
		}
		t.Fatalf("OpenSystem() error = %v; want a usable KVM system", err)
	}
	defer closeTestResource(t, "system", system.Close)

	vm, err := system.NewVM()
	if err != nil {
		t.Fatalf("NewVM() error = %v", err)
	}
	defer closeTestResource(t, "VM", vm.Close)

	if err := vm.LoadGuest(0, []byte{0xF4}); err != nil {
		t.Fatalf("LoadGuest() error = %v", err)
	}

	vcpu, err := vm.NewVCPU(0)
	if err != nil {
		t.Fatalf("NewVCPU(0) error = %v", err)
	}
	defer closeTestResource(t, "vCPU", vcpu.Close)

	if err := vcpu.ConfigureRealMode(0); err != nil {
		t.Fatalf("ConfigureRealMode(0) error = %v", err)
	}

	exit, err := vcpu.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if exit.Reason != 5 { // KVM_EXIT_HLT from the Linux KVM UAPI.
		t.Fatalf("Run() exit reason = %d, want KVM_EXIT_HLT (5)", exit.Reason)
	}
}

func isKVMUnavailable(err error) bool {
	var environmentError *EnvironmentError
	if !errors.As(err, &environmentError) || environmentError.operation != "open /dev/kvm" {
		return false
	}

	return errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ENODEV) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.EPERM)
}

func closeTestResource(t *testing.T, name string, closeFn func() error) {
	t.Helper()
	if err := closeFn(); err != nil {
		t.Errorf("close %s: %v", name, err)
	}
}
