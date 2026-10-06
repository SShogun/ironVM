# IronVM — Engineering Decisions

**Project:** IronVM  
**Format:** Compact Architecture Decision Record  
**Rule:** These decisions remain binding during the 4-hour V1 unless evidence proves one blocks the acceptance contract.

## D001 — Build a KVM VMM, not an emulator

**Decision:** Use Linux KVM through `/dev/kvm`.

**Why:** execution is hardware-virtualized; the project crosses a real kernel/userspace ABI; Go still owns VM lifecycle, guest memory, vCPU state, exit handling and device emulation; the proof fits the time box.

**Rejected:** x86 interpreter, QEMU/libvirt wrapper, bare-metal hypervisor.

## D002 — Linux x86-64 only

Target `linux/amd64` for V1. Depth matters more than portability here. Unsupported platforms should fail clearly rather than pretend to work.

## D003 — Tiny raw real-mode guest

Execute a minimal raw x86 guest from physical address `0x0000`.

This avoids page tables, GDT work, long-mode transitions, ELF parsing and firmware while still proving real guest execution and VM exits.

## D004 — No existing Go KVM wrapper

Implement the required KVM UAPI bindings locally.

Allowed dependency:

```text
golang.org/x/sys/unix
```

Do not import a ready-made Go KVM/VMM package. Consult authoritative KVM docs and Linux UAPI headers and define only what IronVM needs.

## D005 — Contain `unsafe`

`unsafe` is allowed only inside `internal/kvm`.

The KVM ABI necessarily exposes C layouts and shared memory. Keeping `unsafe` localized makes the rest of the system ordinary typed Go.

Acceptance check:

```bash
grep -R '"unsafe"' .
```

should show only ABI-boundary usage plus tests that explicitly validate it.

## D006 — One VM, one vCPU, one memory slot

V1 topology:

```text
VMs:          1
vCPUs:        1
memory slots: 1
guest RAM:    64 KiB
```

No topology abstraction until a second topology is actually required.

## D007 — Port I/O is the device model

Use x86 `IN`/`OUT` instructions and `KVM_EXIT_IO`.

```text
0x00E9  guest -> host console
0x5050  host  -> guest deterministic input
0x00F4  guest -> host result
```

This exposes the full guest → CPU → KVM → userspace → guest round trip without needing MMIO or virtio.

## D008 — Deterministic bidirectional protocol

Host always returns byte `42` on `0x5050`; guest reports it through `0xF4`.

Completion is semantic, not just process exit:

```text
console == "Hi"
host supplied == 42
guest result == 42
final exit == HLT
```

## D009 — Embed the executable guest bytes

Keep the V1 guest payload as a small documented Go byte slice so tests do not depend on NASM.

A readable `.asm` listing may be included as documentation. A generator/assembler pipeline is post-V1 unless time remains after the gate passes.

## D010 — Decode only exits required by V1

Supported:

```text
KVM_EXIT_IO
KVM_EXIT_HLT
```

Every other exit becomes a diagnostic error with the raw exit reason. No giant exit abstraction.

## D011 — RED before GREEN

Every milestone begins with an executable failing test/probe wherever technically possible.

Canonical cycle:

```text
EXPLORE
→ RED realistic regression/acceptance test
→ ARCHITECT contract
→ GREEN minimum implementation
→ targeted tests
→ full regression
→ independent review
→ evidence report
→ COMMIT / STOP
```

Never weaken a RED test merely to make the implementation pass.

## D012 — Separate pure and KVM integration tests

Pure tests must run without `/dev/kvm`.

KVM integration tests may skip only when KVM is genuinely absent/inaccessible. Assertion failures on a KVM-capable host are failures, never environment skips.

## D013 — Linux UAPI is the ABI authority

Before binding a structure or ioctl, inspect authoritative KVM documentation and the host UAPI header, commonly:

```text
/usr/include/linux/kvm.h
```

Do not guess layout, offsets or ioctl encodings from memory or an old repository.

Notes must record the fields/layout actually required before implementation starts.

## D014 — Documentation before commands, commands before code

Human workflow:

```text
read authoritative docs/header
→ state what the operation must do
→ write concise notes/invariants
→ identify the exact command/API call
→ execute it
→ inspect the complete result
→ then implement
```

Commands are evidence-producing experiments, not rituals.

## D015 — Smallest possible implementation per phase

A phase may implement only what its current RED test requires plus unavoidable cleanup.

Examples:

- HLT phase does not implement I/O.
- I/O phase does not add MMIO.
- trace phase does not add a logging framework.

This protects the 4-hour boundary.

## D016 — Two failed repair attempts, then re-diagnose

For one root-cause hypothesis:

```text
failure
→ inspect evidence
→ fix attempt 1
→ retest
→ fix attempt 2
→ retest
→ still failing: STOP implementation
→ reopen docs/header/runtime state
→ form a new hypothesis
```

A required failure may not pass through a milestone gate unresolved.

## D017 — Phase gates are hard gates

A phase passes only when:

1. its mandatory RED test was observed failing for the expected reason;
2. the minimal implementation turns it GREEN;
3. targeted tests pass;
4. relevant full regression passes;
5. independent review has no unresolved blocker;
6. evidence is recorded.

"The code looks right" is not a gate.

## D018 — Codex is conductor, not default implementer

The root Codex session owns synthesis, architecture and gate decisions.

Use native Codex subagents for bounded research, header/API investigation, repository mapping, test discovery, focused debugging and independent verification.

Use OMX `$team` only for durable, independently editable implementation lanes.

## D019 — Model routing

For this project:

```text
gpt-6.1-sol
    conductor / architect / critic / verifier / final reviewer

gpt-6-luna
    explorer / researcher / test engineer / executor /
    debugger / writer / mechanical checks
```

Use the stronger model for judgment and synthesis; use Luna aggressively for bounded evidence gathering and implementation work.

No agent may certify its own implementation.

## D020 — Bounded concurrency

Prefer **2–5 active workers** for IronVM. Do not exceed the established ~6–8 ceiling without a concrete reason.

Bad:

```text
8 agents all reading kvm.h
```

Good:

```text
Explorer A: VM/memory lifecycle
Explorer B: vCPU/register initialization
Explorer C: kvm_run/KVM_EXIT_IO layout
Test engineer: RED acceptance design
```

## D021 — Independent verification is mandatory

Verification order:

```text
targeted tests
→ go test ./...
→ go vet ./...
→ real KVM demo
→ code review
→ ABI/unsafe audit
→ fresh-run reproducibility check
```

Implementer summaries alone do not count as evidence.

## D022 — No silent software fallback

If KVM is unavailable, fail with a clear environment error. Do not fall back to software emulation; that would make the demo ambiguous.

## D023 — Observability is part of correctness

The V1 demo prints the VM entry/exit sequence, I/O direction, port, values, and final result.

This proves the control handoff visually without creating a logging subsystem.

## D024 — Resource cleanup is explicit

FDs close exactly once. mmaps unmap exactly once. Guest-memory backing remains alive while registered and executable by KVM. Partial-construction error paths clean up already-created resources.

## D025 — V1 ends at the proof

Once the deterministic bidirectional I/O demo, tests, audits and README are complete, stop.

MMIO, CPUID, long mode, ELF loading, multi-vCPU, IRQ injection, virtio and Linux boot are post-V1 only.
