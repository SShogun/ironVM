package kvm

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestOpenSystem(t *testing.T) {
	system, err := OpenSystem()
	if err != nil {
		var environmentError *EnvironmentError
		if !errors.As(err, &environmentError) {
			t.Fatalf("OpenSystem() error = %v, want EnvironmentError", err)
		}
		if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
			if !strings.Contains(err.Error(), "Linux/amd64") {
				t.Fatalf("OpenSystem() error = %v, want unsupported-host detail", err)
			}
			return
		}
		t.Skipf("KVM is unavailable in this environment: %v", err)
	}

	if err := system.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if got := system.APIVersion(); got != apiVersion {
		t.Fatalf("APIVersion() = %d, want %d", got, apiVersion)
	}
}
