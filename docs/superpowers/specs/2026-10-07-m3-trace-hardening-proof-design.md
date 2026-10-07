# M3 Trace, Hardening, and Proof Design

## Goal

Finish IronVM V1 by making the existing KVM demo's behavior observable and reproducible, and by auditing its resource ownership, error paths, ABI assumptions, and scope.

## Current baseline

M0 through M2 have produced a working KVM execution path and bidirectional port-I/O demo in the current workspace. M1/M2 implementation files are currently untracked; M3 must preserve that work and build on the existing interfaces. The repository's `docs/PLAN.md` and `docs/ARCHITECTURE.md` remain the detailed source of acceptance requirements and invariants.

## Design

Keep the current package boundaries and add only what closes the V1 acceptance gap. First, express the demo's required behavior through semantic assertions: the output reports the KVM API, VM/vCPU setup, console bytes, host input 42, guest result 42, HLT, and final PASS status. Assertions should check meaning rather than exact whitespace.

Add minimal human-readable trace output at the existing execution boundary. It should show each VM entry and exit, I/O direction, port, value, cumulative exit count, and final guest result. Keep formatting local and avoid a logging dependency.

Review resource ownership and error handling across KVM and VMM construction and execution. Ensure owned file descriptors and mappings are released once, partially constructed objects clean up acquired resources, and guest RAM remains alive for the full KVM access lifetime. Preserve operation context in errors and surface unexpected exits.

## Boundaries

- Keep Linux x86-64 with `/dev/kvm` as the supported target.
- Keep `unsafe` confined to the KVM ABI boundary.
- Do not add MMIO, virtio, networking, disks, ELF loading, paging, long mode, or multiple vCPUs.
- Add no external logger or VMM framework dependency.
- Preserve the current guest protocol and expected values from `docs/ARCHITECTURE.md`.

## Verification and release gate

Run targeted tests, `go test ./...`, and `go vet ./...`. Run the real KVM demo and verify the semantic acceptance assertions, including the transcript's final PASS. Review all unsafe use, ABI sizes/alignment/ioctl arguments, `kvm_run` offset arithmetic, memory bounds, and lifetimes. Audit dependency and guest-byte provenance, search for scope creep, and confirm the README explains architecture and host requirements. Reproduce the demo from a fresh shell/repository state and capture its transcript. M3 is complete only when every mandatory gate in `docs/PLAN.md` is satisfied; unresolved correctness or ABI/unsafe blockers prevent release.
