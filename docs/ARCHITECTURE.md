# IronVM — Architecture

**Language:** Go  
**Target:** Linux x86-64 with `/dev/kvm`  
**Scope:** Tiny hardware-accelerated x86 VMM built directly against the Linux KVM userspace API  
**Constraint:** 4-hour build; no QEMU/libvirt/VMM framework; no Linux guest boot

## 1. Purpose

IronVM makes the virtualization boundary visible. A small Go process will open `/dev/kvm`, validate the KVM API, create a VM, allocate/register guest physical memory, create one vCPU, initialize real-mode x86 state, copy a tiny raw x86 guest into RAM, enter the guest via `KVM_RUN`, handle VM exits in Go, emulate a tiny port-I/O protocol, and print a deterministic execution trace.

The demo claim is:

> A Go program controls guest physical memory and virtual CPU state, enters hardware-assisted x86 execution, regains control on KVM exits, and emulates guest-visible hardware from userspace.

## 2. Non-goals

IronVM V1 will **not** implement Linux boot, BIOS/UEFI, ELF loading, protected/long mode, paging, multiple vCPUs, interrupts/APIC, PCI, virtio, disks, networking, a general-purpose emulator, GUI/TUI, or portability beyond Linux x86-64.

If a non-goal appears necessary, reduce the design before expanding scope.

## 3. System boundary

```text
┌───────────────────────────────────────────────────────────┐
│                       Host Linux                          │
│                                                           │
│  ┌─────────────────────────────────────────────────────┐  │
│  │                    IronVM (Go)                     │  │
│  │                                                     │  │
│  │ CLI/trace                                           │  │
│  │    │                                                │  │
│  │    ▼                                                │  │
│  │ VMM lifecycle ──► KVM ABI ──► ioctl/mmap           │  │
│  │    │                    │                          │  │
│  │    ├── guest RAM       ├── VM fd                  │  │
│  │    └── exit dispatcher └── vCPU fd + kvm_run      │  │
│  │             │                                       │  │
│  │             └──► port-I/O devices                  │  │
│  └───────────────────────┬─────────────────────────────┘  │
│                          │ /dev/kvm                       │
│                          ▼                                │
│                    Linux KVM subsystem                    │
│                          │                                │
│                          ▼                                │
│               CPU virtualization hardware                │
│                          │                                │
│                          ▼                                │
│                    raw x86 guest code                     │
└───────────────────────────────────────────────────────────┘
```

## 4. Repository shape

```text
ironvm/
├── cmd/
│   └── ironvm/
│       └── main.go
├── internal/
│   ├── kvm/
│   │   ├── abi_linux_amd64.go
│   │   ├── ioctl_linux.go
│   │   ├── system_linux.go
│   │   ├── vm_linux.go
│   │   └── vcpu_linux.go
│   ├── guest/
│   │   └── program.go
│   ├── device/
│   │   └── portio.go
│   └── trace/
│       └── trace.go
├── tests/
│   └── integration_linux_test.go
├── ARCHITECTURE.md
├── DECISIONS.md
├── PLAN.md
├── go.mod
└── README.md
```

Do not preserve a package merely because it appears in this tree. Collapse trivial packages if they add no real boundary.

## 5. Runtime flow

```text
main
 │
 ├─► open /dev/kvm
 ├─► KVM_GET_API_VERSION
 ├─► KVM_CREATE_VM
 ├─► mmap anonymous host memory
 ├─► KVM_SET_USER_MEMORY_REGION
 ├─► copy guest bytes at GPA 0x0000
 ├─► KVM_CREATE_VCPU
 ├─► KVM_GET_VCPU_MMAP_SIZE
 ├─► mmap shared kvm_run region
 ├─► KVM_GET_SREGS / KVM_SET_SREGS
 ├─► KVM_SET_REGS (RIP=0, RFLAGS=0x2)
 └─► execution loop
         ├─► ioctl(KVM_RUN)
         ├─ KVM_EXIT_IO  ─► decode ─► PortIO ─► resume
         ├─ KVM_EXIT_HLT ─► normal completion
         └─ other        ─► diagnostic failure
```

## 6. Core components

### 6.1 `internal/kvm` — ABI boundary

Responsibilities:

- KVM ioctl numbers/constants;
- Go representations of only required KVM structures;
- fd ownership;
- raw ioctl calls;
- guest RAM and `kvm_run` mmap handling;
- converting kernel errors into contextual Go errors;
- containing all `unsafe` usage.

Rule:

> `unsafe` is allowed only at the KVM ABI boundary and must not leak into device, guest, trace, or CLI logic.

Minimal ABI surface:

```text
KVM_GET_API_VERSION
KVM_CREATE_VM
KVM_SET_USER_MEMORY_REGION
KVM_CREATE_VCPU
KVM_GET_VCPU_MMAP_SIZE
KVM_GET_SREGS
KVM_SET_SREGS
KVM_SET_REGS
KVM_RUN
```

Relevant structures are limited to the fields IronVM actually uses from:

```text
kvm_userspace_memory_region
kvm_regs
kvm_sregs
kvm_run
```

Struct sizes/alignment must be checked against the installed Linux UAPI header before being trusted.

### 6.2 `System`

Conceptual contract:

```text
System
├── kvmFD
├── APIVersion()
└── NewVM()
```

Owns `/dev/kvm`, validates API compatibility, creates VM descriptors, and contains no guest policy.

### 6.3 `VM`

Conceptual contract:

```text
VM
├── vmFD
├── guestMemory
├── MapMemory(...)
└── NewVCPU(id)
```

V1 uses exactly one region:

```text
GPA base: 0x0000
Size:     64 KiB
Slot:     0
```

Invariant: every guest physical address exposed by IronVM must resolve inside a registered region.

### 6.4 `VCPU`

Conceptual contract:

```text
VCPU
├── vcpuFD
├── runRegion
├── ConfigureRealMode(entry)
└── Run() -> Exit
```

The vCPU owns the vCPU fd and mmap'd `kvm_run`, configures minimal real-mode state, executes one run step, and normalizes raw exits. It does **not** implement devices.

### 6.5 Guest program

Logical guest:

```asm
mov dx, 0xe9
mov al, 'H'
out dx, al

mov al, 'i'
out dx, al

mov dx, 0x5050
in  al, dx

mov dx, 0xf4
out dx, al

hlt
```

Protocol:

| Port | Direction | Meaning |
|---|---|---|
| `0xE9` | guest → host | console byte |
| `0x5050` | host → guest | deterministic byte `42` |
| `0xF4` | guest → host | guest result/status |

Expected semantic result:

```text
guest prints "Hi"
host supplies 42
guest reports 42
guest halts
```

V1 should embed the executable guest as a documented byte slice so tests do not depend on NASM. A readable `.asm` listing may exist only as documentation/reference.

### 6.6 Exit dispatcher

Normalize raw exits before device handling.

```text
Exit
├── Reason
├── IO?
└── RawReason

IOExit
├── Direction
├── Port
├── Size
├── Count
└── Data
```

```text
KVM_RUN
   ↓
decode exit
   ↓
┌──────────────┬───────────────┬─────────────────┐
│ IO           │ HLT           │ Unexpected      │
│ PortIO       │ complete      │ diagnostic fail │
└──────────────┴───────────────┴─────────────────┘
```

Unexpected exits are never silently retried.

### 6.7 Port-I/O device layer

Behavior:

- `OUT 0xE9`: append/print guest console byte;
- `IN 0x5050`: write byte `42` into the KVM run data area;
- `OUT 0xF4`: capture the guest result;
- unknown port: explicit unsupported-I/O error.

The device layer knows nothing about ioctls.

### 6.8 Trace layer

Trace output is part of the demo. Record VM entries/exits, I/O direction, port, value, cumulative exit count, and final guest result. No logging framework is needed.

## 7. State machine

```text
NEW
 │
 ▼
KVM_OPEN
 │
 ▼
VM_CREATED
 │
 ▼
MEMORY_REGISTERED
 │
 ▼
VCPU_CREATED
 │
 ▼
CPU_CONFIGURED
 │
 ▼
RUNNING
 │
 ├──── I/O exit ────► HANDLE_IO ────► RUNNING
 ├──── HLT exit ────► HALTED
 └──── other ───────► FAILED
```

## 8. Error model

Kernel errors must retain operation context:

```text
get KVM API version: ioctl KVM_GET_API_VERSION: <errno>
create VM: ioctl KVM_CREATE_VM: <errno>
register guest RAM: ioctl KVM_SET_USER_MEMORY_REGION: <errno>
run vCPU: ioctl KVM_RUN: <errno>
```

Environment failures are distinct:

```text
/dev/kvm missing
/dev/kvm permission denied
KVM API mismatch
unsupported host architecture
```

## 9. Testing architecture

### Pure tests

No `/dev/kvm` required. Cover:

- port dispatch;
- unsupported ports;
- host response byte;
- memory bounds helpers;
- exit-decoding helpers that can safely consume synthetic buffers.

### KVM integration tests

Requirements:

```text
GOOS=linux
GOARCH=amd64
/dev/kvm accessible
```

Progression:

```text
RED: KVM probe contract
GREEN: API probe works

RED: minimal guest must reach HLT
GREEN: KVM_EXIT_HLT observed

RED: bidirectional transcript missing
GREEN: "Hi", host 42, guest 42, HLT
```

A KVM test may skip only when the environment genuinely lacks KVM. An assertion failure on a KVM-capable host is a failure, not a skip.

## 10. Correctness invariants

1. `kvm_run` is mapped using the size returned by `KVM_GET_VCPU_MMAP_SIZE`.
2. Registered guest-memory backing remains alive while KVM may access it.
3. ABI structs match Linux UAPI layout for the host.
4. I/O `data_offset`, `size`, and `count` are bounds-checked before memory access.
5. Only required I/O widths are accepted.
6. Unknown VM exits are surfaced.
7. FDs/mmaps are released exactly once.
8. No host pointer is intentionally exposed to the guest.
9. IronVM never indexes outside its registered guest/run buffers.
10. Success requires the expected guest result, not merely a successful `KVM_RUN`.

## 11. Dependency boundary

V1 dependencies:

```text
Go standard library
```

The current implementation uses the standard-library `syscall` package for
Linux KVM operations; `go.mod` declares no external modules.

No Go KVM wrapper. If a tiny syscall/`unsafe.Pointer` ioctl helper is clearer, keep it local to `internal/kvm`.

## 12. V1 completion contract

One command must produce a deterministic proof resembling:

```text
KVM API: 12
guest RAM: 65536 bytes
vCPU: 0

[vm-entry]
[vm-exit] IO OUT port=0x00e9 value=0x48 ('H')
[vm-entry]
[vm-exit] IO OUT port=0x00e9 value=0x69 ('i')
[vm-entry]
[vm-exit] IO IN  port=0x5050 -> 42
[vm-entry]
[vm-exit] IO OUT port=0x00f4 value=42
[vm-entry]
[vm-exit] HLT

guest console: Hi
guest result: 42
status: PASS
```

Anything beyond this is post-V1.
