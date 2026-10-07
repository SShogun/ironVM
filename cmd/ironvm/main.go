package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"ironvm/internal/kvm"
	"ironvm/internal/trace"
	"ironvm/internal/vmm"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(output io.Writer) (retErr error) {
	system, err := kvm.OpenSystem()
	if err != nil {
		return fmt.Errorf("ironvm: open KVM: %w", err)
	}
	defer func() {
		if err := system.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("ironvm: close KVM system: %w", err))
		}
	}()

	api := fmt.Sprintf("KVM API: %d\n", system.APIVersion())
	n, err := io.WriteString(output, api)
	if err != nil {
		return fmt.Errorf("ironvm: write KVM API: %w", err)
	}
	if n != len(api) {
		return fmt.Errorf("ironvm: write KVM API: %w", io.ErrShortWrite)
	}
	if _, err := vmm.RunDemo(system, trace.NewWriter(output)); err != nil {
		return fmt.Errorf("ironvm: run demo: %w", err)
	}
	return nil
}
