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
