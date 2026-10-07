//go:build linux && amd64

package vmm

import (
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"ironvm/internal/kvm"
)

func TestRunDemoPortIOTranscript(t *testing.T) {
	system, err := kvm.OpenSystem()
	if err != nil {
		var environmentError *kvm.EnvironmentError
		if errors.As(err, &environmentError) &&
			strings.Contains(environmentError.Error(), "open /dev/kvm") &&
			isUnavailableKVMErrno(err) {
			t.Skipf("KVM integration environment unavailable: %v", err)
		}
		t.Fatalf("OpenSystem() error = %v, want a usable KVM system", err)
	}
	defer func() {
		if err := system.Close(); err != nil {
			t.Errorf("close KVM system: %v", err)
		}
	}()

	result, err := RunDemo(system)
	if err != nil {
		t.Fatalf("RunDemo() error = %v", err)
	}
	if result.Console != "Hi" {
		t.Errorf("Console = %q, want %q", result.Console, "Hi")
	}
	if result.HostInput != 42 {
		t.Errorf("HostInput = %d, want 42", result.HostInput)
	}
	if result.GuestResult != 42 {
		t.Errorf("GuestResult = %d, want 42", result.GuestResult)
	}
	if !result.HasGuestResult {
		t.Error("HasGuestResult = false, want true")
	}
	if result.ExitReason != 5 {
		t.Errorf("ExitReason = %d, want KVM_EXIT_HLT (5)", result.ExitReason)
	}

	wantEvents := []IOEvent{
		{Direction: "OUT", Port: 0x00E9, Value: 0x48},
		{Direction: "OUT", Port: 0x00E9, Value: 0x69},
		{Direction: "IN", Port: 0x5050, Value: 42},
		{Direction: "OUT", Port: 0x00F4, Value: 42},
	}
	if !reflect.DeepEqual(result.Events, wantEvents) {
		t.Errorf("Events = %#v, want %#v", result.Events, wantEvents)
	}
}

func isUnavailableKVMErrno(err error) bool {
	return errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ENODEV) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.EPERM)
}
