//go:build linux && amd64

package vmm

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"ironvm/internal/kvm"
	"ironvm/internal/trace"
)

func openDemoSystem(t *testing.T) *kvm.System {
	t.Helper()
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
	t.Cleanup(func() {
		if err := system.Close(); err != nil {
			t.Errorf("close KVM system: %v", err)
		}
	})
	return system
}

func TestRunDemoPortIOTranscript(t *testing.T) {
	system := openDemoSystem(t)
	var output bytes.Buffer
	result, err := RunDemo(system, trace.NewWriter(&output))
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

	wantTrace := "guest RAM: 65536 bytes\nvCPU: 0\n" +
		"[vm-entry]\n[vm-exit] IO OUT port=0x00e9 value=0x48 ('H') exits=1\n" +
		"[vm-entry]\n[vm-exit] IO OUT port=0x00e9 value=0x69 ('i') exits=2\n" +
		"[vm-entry]\n[vm-exit] IO IN  port=0x5050 value=0x2a ('*') exits=3\n" +
		"[vm-entry]\n[vm-exit] IO OUT port=0x00f4 value=0x2a ('*') exits=4\n" +
		"[vm-entry]\n[vm-exit] HLT exits=5\n" +
		"guest console: Hi\nhost input: 42\nguest result: 42\nstatus: PASS\n"
	if got := output.String(); got != wantTrace {
		t.Errorf("trace =\n%s\nwant:\n%s", got, wantTrace)
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

// A trace failure at any boundary must stop the demo and remain discoverable.
func TestRunDemoPropagatesTraceErrors(t *testing.T) {
	system := openDemoSystem(t)
	sentinel := errors.New("trace output failed")
	for _, tc := range []struct {
		name   string
		writes int
	}{
		{"setup", 0}, {"entry", 1}, {"OUT", 2}, {"IN", 6}, {"HLT", 10}, {"summary", 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := &failAfterWriter{remaining: tc.writes, err: sentinel}
			result, err := RunDemo(system, trace.NewWriter(output))
			if !errors.Is(err, sentinel) {
				t.Fatalf("RunDemo() error = %v, want %v", err, sentinel)
			}
			if tc.name == "summary" && (result.Console != "Hi" || result.GuestResult != 42 || !result.HasGuestResult) {
				t.Errorf("lost completed typed result: %+v", result)
			}
			if output.attempts != tc.writes+1 {
				t.Errorf("write attempts = %d, want %d", output.attempts, tc.writes+1)
			}
		})
	}
	// Nil system must remain a contextual validation error.
	if _, err := RunDemo(nil, trace.NewWriter(io.Discard)); err == nil {
		t.Fatal("RunDemo(nil) succeeded")
	}
}

type failAfterWriter struct {
	remaining, attempts int
	err                 error
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	w.attempts++
	if w.remaining == 0 {
		return 0, w.err
	}
	w.remaining--
	return len(p), nil
}

func TestRunDemoNilTracer(t *testing.T) {
	system := openDemoSystem(t)
	if _, err := RunDemo(system, nil); err == nil {
		t.Fatal("RunDemo with nil tracer succeeded")
	}
}
