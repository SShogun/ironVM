package main

import (
	"os/exec"
	"strings"
	"testing"
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
