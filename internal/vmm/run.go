// Package vmm coordinates guest execution and the small userspace device model.
package vmm

import (
	"errors"
	"fmt"

	"ironvm/internal/device"
	"ironvm/internal/guest"
	"ironvm/internal/kvm"
	"ironvm/internal/trace"
)

// IOEvent records one completed byte-wide guest port-I/O operation.
type IOEvent struct {
	Direction string
	Port      uint16
	Value     byte
}

// Result is the semantic result of the M2 guest protocol.
type Result struct {
	Console        string
	HostInput      byte
	GuestResult    byte
	HasGuestResult bool
	ExitReason     uint32
	Events         []IOEvent
}

// RunDemo loads and executes the raw M2 guest on an already-open KVM system.
// The caller retains ownership of system; this function owns the VM and vCPU.
func RunDemo(system *kvm.System, tracer *trace.Writer) (result Result, retErr error) {
	if system == nil {
		return Result{}, errors.New("run demo: KVM system is nil")
	}

	if tracer == nil {
		return Result{}, errors.New("run demo: trace writer is nil")
	}

	vm, err := system.NewVM()
	if err != nil {
		return Result{}, fmt.Errorf("run demo: create VM: %w", err)
	}
	defer func() {
		if err := vm.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("run demo: close VM: %w", err))
		}
	}()

	if err := vm.LoadGuest(0, guest.DemoProgram()); err != nil {
		return Result{}, fmt.Errorf("run demo: load guest: %w", err)
	}

	vcpu, err := vm.NewVCPU(0)
	if err != nil {
		return Result{}, fmt.Errorf("run demo: create vCPU: %w", err)
	}
	defer func() {
		if err := vcpu.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("run demo: close vCPU: %w", err))
		}
	}()

	if err := vcpu.ConfigureRealMode(0); err != nil {
		return Result{}, fmt.Errorf("run demo: configure real-mode vCPU: %w", err)
	}

	if err := tracer.Setup(64*1024, 0); err != nil {
		return Result{}, fmt.Errorf("run demo: trace setup: %w", err)
	}

	portIO := device.NewPortIO()
	for {
		if err := tracer.Entry(); err != nil {
			return Result{}, fmt.Errorf("run demo: trace entry: %w", err)
		}
		exit, err := vcpu.Run()
		if err != nil {
			return Result{}, fmt.Errorf("run demo: enter guest: %w", err)
		}

		switch exit.Reason {
		case kvm.ExitReasonIO:
			if exit.IO == nil {
				return Result{}, errors.New("run demo: KVM_EXIT_IO has no decoded payload")
			}
			if exit.IO.Size != 1 || exit.IO.Count != 1 {
				return Result{}, fmt.Errorf("run demo: unsupported I/O width/count: size=%d count=%d", exit.IO.Size, exit.IO.Count)
			}

			switch exit.IO.Direction {
			case kvm.IODirectionOut:
				if len(exit.IO.Data) != 1 {
					return Result{}, fmt.Errorf("run demo: OUT payload has %d bytes, want 1", len(exit.IO.Data))
				}
				value := exit.IO.Data[0]
				if err := portIO.Write(exit.IO.Port, value); err != nil {
					return Result{}, fmt.Errorf("run demo: dispatch OUT: %w", err)
				}
				result.Events = append(result.Events, IOEvent{Direction: "OUT", Port: exit.IO.Port, Value: value})

			case kvm.IODirectionIn:
				value, err := portIO.Read(exit.IO.Port)
				if err != nil {
					return Result{}, fmt.Errorf("run demo: dispatch IN: %w", err)
				}
				if err := vcpu.CompleteIO(exit, []byte{value}); err != nil {
					return Result{}, fmt.Errorf("run demo: complete IN: %w", err)
				}
				result.HostInput = value
				result.Events = append(result.Events, IOEvent{Direction: "IN", Port: exit.IO.Port, Value: value})

			default:
				return Result{}, fmt.Errorf("run demo: unsupported I/O direction %d", exit.IO.Direction)
			}

			event := result.Events[len(result.Events)-1]
			if err := tracer.IOExit(event.Direction, event.Port, event.Value); err != nil {
				return Result{}, fmt.Errorf("run demo: trace I/O exit: %w", err)
			}

		case kvm.ExitReasonHLT:
			if err := tracer.HLT(); err != nil {
				return Result{}, fmt.Errorf("run demo: trace HLT exit: %w", err)
			}
			result.Console = portIO.Console()
			result.GuestResult, result.HasGuestResult = portIO.Result()
			result.ExitReason = exit.Reason
			if result.Console != "Hi" {
				return result, fmt.Errorf("run demo: guest console = %q, want %q", result.Console, "Hi")
			}
			if result.HostInput != 42 {
				return result, fmt.Errorf("run demo: host input = %d, want 42", result.HostInput)
			}
			if !result.HasGuestResult || result.GuestResult != 42 {
				return result, fmt.Errorf("run demo: guest result = %d (seen=%t), want 42", result.GuestResult, result.HasGuestResult)
			}
			if err := tracer.Summary(result.Console, result.HostInput, result.GuestResult); err != nil {
				return result, fmt.Errorf("run demo: trace summary: %w", err)
			}
			return result, nil

		default:
			return Result{}, fmt.Errorf("run demo: unsupported KVM exit reason %d", exit.Reason)
		}
	}
}
