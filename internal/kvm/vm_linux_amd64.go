//go:build linux && amd64

package kvm

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	kvmCreateVM            = 0xAE01
	kvmGetVCPUMmapSize     = 0xAE04
	kvmSetUserMemoryRegion = 0x4020AE46
	guestRAMSize           = 64 * 1024
)

var (
	errVMClosed = errors.New("KVM VM is already closed")
	errVMNil    = errors.New("KVM VM is nil")
)

// userspaceMemoryRegion mirrors struct kvm_userspace_memory_region in
// /usr/include/linux/kvm.h. Keep the field order and widths in sync with UAPI.
type userspaceMemoryRegion struct {
	slot          uint32
	flags         uint32
	guestPhysAddr uint64
	memorySize    uint64
	userspaceAddr uint64
}

// VM owns the VM fd and the host mapping used as its guest physical memory.
// The mapping must outlive the KVM memory slot that refers to it.
type VM struct {
	fd          int
	memory      []byte
	runMmapSize int
	closed      bool
}

// NewVM creates a default VM, registers 64 KiB of RAM at guest physical
// address zero, and records the kernel-provided vCPU run mapping size.
func (s *System) NewVM() (*VM, error) {
	if s == nil {
		return nil, errors.New("create KVM VM: system is nil")
	}
	if s.closed {
		return nil, fmt.Errorf("create KVM VM: %w", errSystemClosed)
	}

	vmFDValue, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), kvmCreateVM, 0)
	if errno != 0 {
		return nil, fmt.Errorf("ioctl KVM_CREATE_VM: %w", errno)
	}
	vmFD := int(vmFDValue)

	memory, err := syscall.Mmap(-1, 0, guestRAMSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		return nil, cleanupVMConstruction(vmFD, nil, fmt.Errorf("mmap 64 KiB guest RAM: %w", err))
	}

	region := userspaceMemoryRegion{
		slot:          0,
		guestPhysAddr: 0,
		memorySize:    uint64(len(memory)),
		userspaceAddr: uint64(uintptr(unsafe.Pointer(&memory[0]))),
	}
	_, _, errno = syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(vmFD),
		kvmSetUserMemoryRegion,
		uintptr(unsafe.Pointer(&region)),
	)
	runtime.KeepAlive(&region)
	runtime.KeepAlive(memory)
	if errno != 0 {
		return nil, cleanupVMConstruction(vmFD, memory, fmt.Errorf("ioctl KVM_SET_USER_MEMORY_REGION: %w", errno))
	}

	runMmapSize, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.fd), kvmGetVCPUMmapSize, 0)
	if errno != 0 {
		return nil, cleanupVMConstruction(vmFD, memory, fmt.Errorf("ioctl KVM_GET_VCPU_MMAP_SIZE: %w", errno))
	}
	if runMmapSize == 0 || runMmapSize > uintptr(int(^uint(0)>>1)) {
		return nil, cleanupVMConstruction(vmFD, memory, fmt.Errorf("KVM_GET_VCPU_MMAP_SIZE returned invalid size %d", runMmapSize))
	}

	return &VM{fd: vmFD, memory: memory, runMmapSize: int(runMmapSize)}, nil
}

// LoadGuest copies code into the registered guest RAM at the supplied GPA.
func (vm *VM) LoadGuest(gpa uint64, code []byte) error {
	if vm == nil {
		return errVMNil
	}
	if vm.closed {
		return errVMClosed
	}
	if len(vm.memory) == 0 {
		return errors.New("load guest: guest RAM is unavailable")
	}
	capacity := uint64(len(vm.memory))
	if gpa > capacity || uint64(len(code)) > capacity-gpa {
		return fmt.Errorf("load guest: GPA %#x with %d bytes exceeds guest RAM [0, %#x)", gpa, len(code), capacity)
	}
	copy(vm.memory[int(gpa):], code)
	return nil
}

// Close releases the VM fd before unmapping its guest RAM backing.
func (vm *VM) Close() error {
	if vm == nil {
		return errVMNil
	}
	if vm.closed {
		return errVMClosed
	}
	// Remove the slot before releasing the backing mapping. A vCPU fd may keep
	// the VM alive after its VM fd is closed, so closing the fd alone does not
	// guarantee that KVM has stopped using this userspace address.
	region := userspaceMemoryRegion{slot: 0}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(vm.fd),
		kvmSetUserMemoryRegion,
		uintptr(unsafe.Pointer(&region)),
	)
	runtime.KeepAlive(&region)
	if errno != 0 {
		return fmt.Errorf("ioctl KVM_SET_USER_MEMORY_REGION (remove slot 0): %w", errno)
	}
	vm.closed = true

	var closeErr error
	if err := syscall.Close(vm.fd); err != nil {
		closeErr = fmt.Errorf("close KVM VM fd: %w", err)
	}
	if len(vm.memory) != 0 {
		if err := syscall.Munmap(vm.memory); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("munmap guest RAM: %w", err))
		} else {
			vm.memory = nil
		}
	}
	return closeErr
}

func cleanupVMConstruction(vmFD int, memory []byte, cause error) error {
	if err := syscall.Close(vmFD); err != nil {
		cause = errors.Join(cause, fmt.Errorf("close KVM VM fd after construction failure: %w", err))
	}
	if len(memory) != 0 {
		if err := syscall.Munmap(memory); err != nil {
			cause = errors.Join(cause, fmt.Errorf("munmap guest RAM after construction failure: %w", err))
		}
	}
	return cause
}
