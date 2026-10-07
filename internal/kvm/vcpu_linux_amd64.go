//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	kvmCreateVCPU = 0xAE41
	kvmRun        = 0xAE80
	kvmGetSRegs   = 0x8138AE83
	kvmSetSRegs   = 0x4138AE84
	kvmSetRegs    = 0x4090AE82

	kvmRunExitOffset = 8
	kvmRunExitSize   = 4

	kvmRunIOOffset          = 32
	kvmRunIODirectionOffset = kvmRunIOOffset
	kvmRunIOSizeOffset      = kvmRunIOOffset + 1
	kvmRunIOPortOffset      = kvmRunIOOffset + 2
	kvmRunIOCountOffset     = kvmRunIOOffset + 4
	kvmRunIODataOffset      = kvmRunIOOffset + 8
	kvmRunIOHeaderSize      = 16
)

const (
	ExitReasonIO  uint32 = 2
	ExitReasonHLT uint32 = 5

	IODirectionIn  uint8 = 0
	IODirectionOut uint8 = 1
)

// kvmRegs matches the x86 UAPI struct kvm_regs in asm/kvm.h.
type kvmRegs struct {
	rax, rbx, rcx, rdx uint64
	rsi, rdi, rsp, rbp uint64
	r8, r9, r10, r11   uint64
	r12, r13, r14, r15 uint64
	rip, rflags        uint64
}

// kvmSegment matches the x86 UAPI struct kvm_segment in asm/kvm.h.
type kvmSegment struct {
	base     uint64
	limit    uint32
	selector uint16
	typeBits uint8
	present  uint8
	dpl      uint8
	db       uint8
	s        uint8
	l        uint8
	g        uint8
	avl      uint8
	unusable uint8
	padding  uint8
}

// kvmDtable matches the x86 UAPI struct kvm_dtable in asm/kvm.h.
type kvmDtable struct {
	base    uint64
	limit   uint16
	padding [3]uint16
}

// kvmSRegs matches the x86 UAPI struct kvm_sregs in asm/kvm.h.
type kvmSRegs struct {
	cs, ds, es, fs, gs, ss kvmSegment
	tr, ldt                kvmSegment
	gdt, idt               kvmDtable
	cr0, cr2, cr3, cr4     uint64
	cr8, efer, apicBase    uint64
	interruptBitmap        [4]uint64
}

// Exit describes a terminal or otherwise surfaced exit from KVM_RUN.
type Exit struct {
	Reason uint32
	IO     *IOExit
}

// IOExit describes one userspace port-I/O exit. Data contains a copy of the
// packed payload for OUT exits; IN payloads are supplied with CompleteIO.
type IOExit struct {
	Direction uint8
	Port      uint16
	Size      uint8
	Count     uint32
	Data      []byte

	dataOffset uint64
}

// VCPU owns a vCPU file descriptor and its shared kvm_run mapping.
type VCPU struct {
	vm        *VM
	fd        int
	run       []byte
	closed    bool
	pendingIO *IOExit
}

// NewVCPU creates a vCPU in the VM and maps its kernel-sized kvm_run area.
func (vm *VM) NewVCPU(id int) (*VCPU, error) {
	if vm == nil {
		return nil, errors.New("create vCPU: nil VM")
	}
	if id < 0 {
		return nil, fmt.Errorf("create vCPU: negative slot id %d", id)
	}
	if vm.closed {
		return nil, errors.New("create vCPU: VM is closed")
	}
	if vm.runMmapSize < kvmRunExitOffset+kvmRunExitSize {
		return nil, fmt.Errorf("create vCPU: invalid KVM vCPU mmap size %d", vm.runMmapSize)
	}

	fd, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(vm.fd), kvmCreateVCPU, uintptr(id))
	if errno != 0 {
		return nil, fmt.Errorf("create vCPU %d (KVM_CREATE_VCPU): %w", id, errno)
	}

	run, err := syscall.Mmap(int(fd), 0, vm.runMmapSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		closeErr := syscall.Close(int(fd))
		if closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close vCPU fd after mmap failure: %w", closeErr))
		}
		return nil, fmt.Errorf("map vCPU %d kvm_run region (%d bytes): %w", id, vm.runMmapSize, err)
	}
	return &VCPU{vm: vm, fd: int(fd), run: run}, nil
}

// ConfigureRealMode sets the vCPU entry point while retaining its reset control state.
func (vcpu *VCPU) ConfigureRealMode(entry uint64) error {
	if err := vcpu.ensureOpen("configure real-mode vCPU"); err != nil {
		return err
	}

	var sregs kvmSRegs
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(vcpu.fd), kvmGetSRegs, uintptr(unsafe.Pointer(&sregs))); errno != 0 {
		runtime.KeepAlive(&sregs)
		return fmt.Errorf("get vCPU special registers (KVM_GET_SREGS): %w", errno)
	}
	runtime.KeepAlive(&sregs)
	// KVM creates an x86 vCPU in real mode. Keep the reset segment attributes
	// and all control registers, using a zero-based CS so RIP is the guest entry.
	sregs.cs.base = 0
	sregs.cs.selector = 0
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(vcpu.fd), kvmSetSRegs, uintptr(unsafe.Pointer(&sregs))); errno != 0 {
		runtime.KeepAlive(&sregs)
		return fmt.Errorf("set vCPU special registers (KVM_SET_SREGS): %w", errno)
	}
	runtime.KeepAlive(&sregs)

	regs := kvmRegs{rip: entry, rflags: 2}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(vcpu.fd), kvmSetRegs, uintptr(unsafe.Pointer(&regs))); errno != 0 {
		runtime.KeepAlive(&regs)
		return fmt.Errorf("set vCPU registers (KVM_SET_REGS): %w", errno)
	}
	runtime.KeepAlive(&regs)
	return nil
}

// Run enters the guest once and returns the exit reason written to kvm_run.
func (vcpu *VCPU) Run() (Exit, error) {
	if err := vcpu.ensureOpen("run vCPU"); err != nil {
		return Exit{}, err
	}
	if len(vcpu.run) < kvmRunExitOffset+kvmRunExitSize {
		return Exit{}, fmt.Errorf("run vCPU: kvm_run mapping too short: %d bytes", len(vcpu.run))
	}
	if vcpu.pendingIO != nil {
		return Exit{}, errors.New("run vCPU: pending KVM_EXIT_IO input has not been completed")
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(vcpu.fd), kvmRun, 0); errno != 0 {
		return Exit{}, fmt.Errorf("run vCPU (KVM_RUN): %w", errno)
	}
	exit, err := decodeExit(vcpu.run)
	if err != nil {
		return Exit{}, fmt.Errorf("decode KVM_RUN exit: %w", err)
	}
	if exit.IO != nil && exit.IO.Direction == IODirectionIn {
		pending := *exit.IO
		vcpu.pendingIO = &pending
	}
	return exit, nil
}

// CompleteIO supplies an exact-size response for a KVM_EXIT_IO_IN exit. The
// bytes are copied into the shared kvm_run mapping and consumed by KVM on the
// next Run call.
func (vcpu *VCPU) CompleteIO(exit Exit, input []byte) error {
	if err := vcpu.ensureOpen("complete vCPU I/O"); err != nil {
		return err
	}
	if exit.Reason != ExitReasonIO || exit.IO == nil {
		return errors.New("complete vCPU I/O: exit is not KVM_EXIT_IO")
	}
	io := exit.IO
	if io.Direction != IODirectionIn {
		return errors.New("complete vCPU I/O: exit direction is not IN")
	}
	if vcpu.pendingIO == nil || !sameIOExit(vcpu.pendingIO, io) {
		return errors.New("complete vCPU I/O: exit is not the pending IN exit for this vCPU")
	}
	pending := vcpu.pendingIO
	start, end, err := ioPayloadRange(len(vcpu.run), pending.dataOffset, pending.Size, pending.Count)
	if err != nil {
		return fmt.Errorf("complete vCPU I/O: %w", err)
	}
	if len(input) != end-start {
		return fmt.Errorf("complete vCPU I/O: response has %d bytes, want exactly %d", len(input), end-start)
	}
	copy(vcpu.run[start:end], input)
	vcpu.pendingIO = nil
	return nil
}

func decodeExit(run []byte) (Exit, error) {
	if len(run) < kvmRunExitOffset+kvmRunExitSize {
		return Exit{}, fmt.Errorf("kvm_run mapping too short for exit reason: %d bytes", len(run))
	}
	reason := binary.LittleEndian.Uint32(run[kvmRunExitOffset : kvmRunExitOffset+kvmRunExitSize])
	exit := Exit{Reason: reason}
	if reason != ExitReasonIO {
		return exit, nil
	}
	if len(run) < kvmRunIOOffset+kvmRunIOHeaderSize {
		return Exit{}, fmt.Errorf("kvm_run mapping too short for KVM_EXIT_IO metadata: %d bytes", len(run))
	}
	io := &IOExit{
		Direction:  run[kvmRunIODirectionOffset],
		Size:       run[kvmRunIOSizeOffset],
		Port:       binary.LittleEndian.Uint16(run[kvmRunIOPortOffset : kvmRunIOPortOffset+2]),
		Count:      binary.LittleEndian.Uint32(run[kvmRunIOCountOffset : kvmRunIOCountOffset+4]),
		dataOffset: binary.LittleEndian.Uint64(run[kvmRunIODataOffset : kvmRunIODataOffset+8]),
	}
	if io.Direction != IODirectionIn && io.Direction != IODirectionOut {
		return Exit{}, fmt.Errorf("KVM_EXIT_IO has unknown direction %d", io.Direction)
	}
	start, end, err := ioPayloadRange(len(run), io.dataOffset, io.Size, io.Count)
	if err != nil {
		return Exit{}, fmt.Errorf("invalid KVM_EXIT_IO payload: %w", err)
	}
	if io.Direction == IODirectionOut {
		io.Data = append([]byte(nil), run[start:end]...)
	}
	exit.IO = io
	return exit, nil
}

func ioPayloadRange(runLen int, dataOffset uint64, size uint8, count uint32) (int, int, error) {
	if size == 0 || count == 0 {
		return 0, 0, fmt.Errorf("invalid KVM_EXIT_IO payload dimensions: size=%d count=%d", size, count)
	}
	if dataOffset < kvmRunIOOffset+kvmRunIOHeaderSize {
		return 0, 0, fmt.Errorf("KVM_EXIT_IO data_offset %d overlaps kvm_run metadata", dataOffset)
	}
	length := uint64(size) * uint64(count)
	if dataOffset > uint64(runLen) || length > uint64(runLen)-dataOffset {
		return 0, 0, fmt.Errorf("KVM_EXIT_IO payload range offset=%d length=%d exceeds kvm_run mapping size %d", dataOffset, length, runLen)
	}
	start := int(dataOffset)
	return start, start + int(length), nil
}

func sameIOExit(a, b *IOExit) bool {
	return a.Direction == b.Direction && a.Port == b.Port && a.Size == b.Size && a.Count == b.Count && a.dataOffset == b.dataOffset
}

// Close unmaps the shared run structure and closes the vCPU fd.
func (vcpu *VCPU) Close() error {
	if vcpu == nil || vcpu.closed {
		return errors.New("vCPU is already closed")
	}
	vcpu.closed = true
	var closeErr error
	if vcpu.run != nil {
		if err := syscall.Munmap(vcpu.run); err != nil {
			closeErr = fmt.Errorf("unmap vCPU kvm_run region: %w", err)
		}
		vcpu.run = nil
	}
	if err := syscall.Close(vcpu.fd); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("close vCPU fd: %w", err))
	}
	return closeErr
}

func (vcpu *VCPU) ensureOpen(operation string) error {
	if vcpu == nil {
		return fmt.Errorf("%s: nil vCPU", operation)
	}
	if vcpu.closed {
		return fmt.Errorf("%s: vCPU is closed", operation)
	}
	return nil
}
