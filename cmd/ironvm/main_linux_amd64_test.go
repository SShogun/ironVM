package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"ironvm/internal/kvm"
)

func TestDemoTranscript(t *testing.T) {
	cmd := exec.Command("go", "run", ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isUnavailableKVMError(output) {
			t.Skipf("KVM unavailable: %v\noutput:\n%s", err, output)
		}
		t.Fatalf("go run . failed: %v\noutput:\n%s", err, output)
	}

	transcript := string(output)
	for _, want := range []string{
		"KVM API:",
		"guest RAM: 65536 bytes",
		"vCPU: 0",
		"guest console: Hi",
		"host input: 42",
		"guest result: 42",
		"HLT",
		"status: PASS",
	} {
		if !strings.Contains(transcript, want) {
			t.Errorf("transcript missing %q\ntranscript:\n%s", want, transcript)
		}
	}
}

func isUnavailableKVMError(output []byte) bool {
	message := string(output)
	if !strings.Contains(message, "open /dev/kvm:") {
		return false
	}
	return strings.Contains(message, "no such file or directory") ||
		strings.Contains(message, "permission denied")
}

func TestIsUnavailableKVMError(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "missing device", output: "open /dev/kvm: no such file or directory", want: true},
		{name: "permission denied", output: "open /dev/kvm: permission denied", want: true},
		{name: "API mismatch", output: "KVM API version mismatch: got 11, want 12"},
		{name: "VM setup error", output: "create VM: permission denied"},
		{name: "unrelated command failure", output: "compile failed: no such file or directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnavailableKVMError([]byte(tt.output)); got != tt.want {
				t.Errorf("isUnavailableKVMError(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

// API and trace output failures must escape run rather than print PASS.
func TestRunPropagatesOutputErrors(t *testing.T) {
	sentinel := errors.New("stdout failed")
	for _, writes := range []int{0, 1, 2, 3, 7, 11, 12} {
		t.Run(fmt.Sprintf("after_%d_writes", writes), func(t *testing.T) {
			writer := &cliFailWriter{remaining: writes, err: sentinel}
			err := run(writer)
			var environmentError *kvm.EnvironmentError
			if errors.As(err, &environmentError) && isUnavailableKVMError([]byte(err.Error())) {
				t.Skipf("KVM unavailable: %v", err)
			}
			if !errors.Is(err, sentinel) {
				t.Fatalf("run() error = %v, want %v", err, sentinel)
			}
			if writer.attempts != writes+1 {
				t.Errorf("write attempts = %d, want %d", writer.attempts, writes+1)
			}
		})
	}
}

type cliFailWriter struct {
	remaining, attempts int
	err                 error
}

func (w *cliFailWriter) Write(p []byte) (int, error) {
	w.attempts++
	if w.remaining == 0 {
		return 0, w.err
	}
	w.remaining--
	return len(p), nil
}
