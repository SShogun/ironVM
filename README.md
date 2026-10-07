# IronVM

IronVM is a small Go demonstration of the Linux KVM boundary. It runs a raw,
documented x86 guest program on one KVM virtual CPU, handles its port-I/O exits
in userspace, and prints the execution trace.

The demo shows Go setting up guest RAM and CPU state, entering hardware-assisted
x86 execution through KVM, and regaining control when the guest exits. The
guest writes `Hi` to the demo console, reads the deterministic byte `42` from
the host, writes that byte back as its result, and halts. `status: PASS` means
the host validated that protocol and the final HLT exit.

## Requirements

- Linux on `amd64` (x86-64).
- A usable `/dev/kvm` device, backed by available CPU virtualization hardware
  with virtualization enabled and exposed to the host.
- The process must be able to open `/dev/kvm` read/write. Device permissions,
  group membership, container configuration, and virtualization settings can
  affect access.
- Go 1.27 or newer, as specified in `go.mod`.

IronVM checks that KVM reports API version 12. It exits with an error when KVM
is absent, inaccessible, or reports a different API version; it has no software
emulation fallback.

## Run

From the repository root:

```sh
go run ./cmd/ironvm
```

A successful real-KVM run produces:

```text
KVM API: 12
guest RAM: 65536 bytes
vCPU: 0
[vm-entry]
[vm-exit] IO OUT port=0x00e9 value=0x48 ('H') exits=1
[vm-entry]
[vm-exit] IO OUT port=0x00e9 value=0x69 ('i') exits=2
[vm-entry]
[vm-exit] IO IN  port=0x5050 value=0x2a ('*') exits=3
[vm-entry]
[vm-exit] IO OUT port=0x00f4 value=0x2a ('*') exits=4
[vm-entry]
[vm-exit] HLT exits=5
guest console: Hi
host input: 42
guest result: 42
status: PASS
```

The transcript records five KVM exits: two console writes, one host input,
one guest result write, and HLT. The I/O protocol is byte-wide and supports
only the ports used by this demo. An unmasked host signal can interrupt
`KVM_RUN`; IronVM surfaces that `EINTR` as a run error and tears down the demo
instead of automatically resuming the vCPU.

## Package boundaries

- `cmd/ironvm` opens KVM, streams the API version and trace to standard output,
  and reports failures to standard error.
- `internal/kvm` owns `/dev/kvm`, VM and vCPU file descriptors, guest RAM and
  the `kvm_run` mapping. It contains the Linux KVM ABI calls and `unsafe` use.
- `internal/vmm` loads the guest, configures one real-mode vCPU, dispatches
  supported I/O exits, validates the demo protocol, and coordinates cleanup.
- `internal/guest` contains the raw x86 machine-code bytes. Its Go source
  includes an assembly-style listing; no assembler is needed to run the demo.
- `internal/device` implements the three-port demo protocol without KVM calls.
- `internal/trace` formats the deterministic execution transcript.

The module uses only the Go standard library and declares no external modules
in `go.mod`. KVM access uses Go's standard-library `syscall` APIs.

## Scope

IronVM V1 demonstrates one VM, one vCPU, 64 KiB of guest RAM, real-mode guest
execution, byte-wide port I/O, and HLT completion. It does not boot an operating
system and does not implement firmware, ELF loading, protected or long mode,
paging, multiple vCPUs, interrupts, MMIO, PCI, virtio, disks, networking, or a
general-purpose x86 emulator.

## References

- [Linux KVM API documentation](https://docs.kernel.org/virt/kvm/api.html)
- [Linux KVM UAPI header](https://github.com/torvalds/linux/blob/master/include/uapi/linux/kvm.h)
- [Intel 64 and IA-32 Architectures Software Developer's Manual, Volumes 2A and 2B](https://cdrdv2-public.intel.com/782156/325383-sdm-vol-2abcd.pdf), for the MOV, IN, OUT, and HLT instructions in the guest listing
