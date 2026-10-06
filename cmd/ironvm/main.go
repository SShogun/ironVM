package main

import (
	"fmt"
	"os"

	"ironvm/internal/kvm"
)

func main() {
	system, err := kvm.OpenSystem()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("KVM API: %d\n", system.APIVersion())
	if err := system.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
