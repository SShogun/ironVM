# M3 Trace, Hardening, and Proof Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish IronVM V1 with a semantic end-to-end proof, a readable execution trace, hardened resource cleanup, and reproducible documentation.

**Architecture:** Keep KVM ABI code in `internal/kvm` and guest orchestration in `internal/vmm`. Add a small standard-library-only trace formatter that receives execution events from the VMM; the CLI supplies the output writer and reports the KVM API version. Extend the CLI integration proof to check transcript meaning, then audit cleanup and document the supported demo.

**Tech Stack:** Go standard library and Linux x86-64 KVM API. Audit `go.mod` and document the dependencies actually used; do not add a dependency just to match stale architecture text.

**Spec:** `docs/superpowers/specs/2026-10-07-m3-trace-hardening-proof-design.md`, with detailed acceptance criteria in `docs/PLAN.md` and invariants in `docs/ARCHITECTURE.md`.

## Global Constraints

- Target Linux x86-64 with accessible `/dev/kvm`.
- Keep `unsafe` inside `internal/kvm`.
- Do not add MMIO, virtio, networking, disks, ELF loading, paging, long mode, or multiple vCPUs.
- Add no external logger or VMM framework dependency.
- Preserve the existing guest protocol: console `Hi`, host response `42`, guest result `42`, and HLT.
- Keep existing M1/M2 work intact; those files are currently untracked in the source checkout.

## Review Focus

- **Construction failure after each acquired resource:** acquired file descriptors and mappings must be released and the originating error retained; add focused coverage where the code can inject a syscall failure without a real KVM host.
- **Close failures and repeated close calls:** cleanup must not silently lose ownership of a resource that remains allocated; preserve explicit, contextual errors.
- **Malformed I/O exit metadata:** existing size, count, and range bounds must remain enforced when trace events are emitted.
- **Trace writer failure:** a failed write must return an error and must not be reported as a successful guest run.
- **KVM unavailable:** integration tests may skip only for a genuine missing/unavailable KVM environment; semantic failures on a KVM host must fail.

---

### Task 1: Specify the end-to-end transcript with a failing CLI integration assertion

**Files:**
- Create: `cmd/ironvm/main_linux_amd64_test.go`
- Test: `cmd/ironvm/main_linux_amd64_test.go`

**Interfaces:**
- Consumes: the existing executable at `cmd/ironvm`.
- Produces: semantic transcript requirements for KVM API, VM/vCPU setup, console, host input, guest result, HLT, and PASS. Do not assert exact whitespace.

- [ ] **Step 1: Add `TestDemoTranscript`** that runs `go run .` from `cmd/ironvm`, captures combined output, and asserts it contains `KVM API:`, `guest RAM: 65536 bytes`, `vCPU: 0`, `guest console: Hi`, `host input: 42`, `guest result: 42`, `HLT`, and `status: PASS`.
- [ ] **Step 2: Run the targeted integration test** with `go test ./cmd/ironvm -run TestDemoTranscript -count=1`; skip only when the command fails specifically because `/dev/kvm` is absent or inaccessible.
- [ ] **Step 3: Confirm RED is the missing transcript behavior** on a KVM-capable host; do not weaken the assertions to make the current output pass.

### Task 2: Add the trace formatter and wire the demo transcript

**Files:**
- Create: `internal/trace/trace.go`
- Test: `internal/trace/trace_test.go`
- Modify: `internal/vmm/run.go`
- Modify: `internal/vmm/integration_linux_amd64_test.go`
- Modify: `cmd/ironvm/main.go`

**Interfaces:**
- `trace.Writer` wraps an `io.Writer`; `NewWriter(io.Writer) *Writer` constructs it; methods `Setup(ramBytes, vcpuID int) error`, `Entry() error`, `IOExit(direction string, port uint16, value byte) error`, `HLT() error`, and `Summary(console string, hostInput, guestResult byte) error` format the transcript and return write errors.
- `vmm.RunDemo(system *kvm.System, tracer *trace.Writer) (Result, error)` reports entry/exit events as they occur. Existing callers in the integration test must pass a writer.
- The CLI opens KVM, prints the API version, runs the demo with stdout tracing, and returns a nonzero exit status on any setup, run, trace, or close error.

- [ ] **Step 1: Add formatter tests** for `Setup`, `Entry`, `IOExit`, `HLT`, and `Summary`, including deterministic exit counts and propagation of an `io.Writer` error.
- [ ] **Step 2: Implement the formatter** using only `fmt` and `io`; print I/O bytes in hex and printable ASCII as described by `docs/ARCHITECTURE.md`.
- [ ] **Step 3: Emit setup, entry, exit, and summary events from the VMM**; include cumulative exit count and preserve the existing typed `Result` and error checks.
- [ ] **Step 4: Wire the CLI to the VMM and stdout tracer** while retaining contextual errors and cleanup of the KVM system; leave `main` as a thin exit-code wrapper around an error-returning `run(io.Writer) error`.
- [ ] **Step 5: Run `go test ./internal/trace ./internal/vmm ./cmd/ironvm`** and confirm formatter tests pass; rerun the KVM transcript assertion for GREEN where `/dev/kvm` is available.

### Task 3: Audit and harden KVM resource ownership and failure paths

**Files:**
- Review and modify as evidence requires: `internal/kvm/system_linux_amd64.go`, `internal/kvm/vm_linux_amd64.go`, `internal/kvm/vcpu_linux_amd64.go`, `internal/vmm/run.go`
- Add/modify targeted tests alongside the affected package files.

**Interfaces:**
- Preserve the current public constructors, `Close` methods, and contextual error behavior unless a demonstrated defect requires a narrow contract change.
- Keep guest memory alive while its KVM memory slot exists; vCPU cleanup must precede VM slot removal and backing-memory unmap.

- [ ] **Step 1: Inventory every fd and mmap acquisition and release** in System, VM, vCPU, and RunDemo; record ownership and deferred cleanup order in the review notes.
- [ ] **Step 2: Add a focused failing test for each confirmed cleanup defect**, using narrow syscall seams only where needed to induce partial-construction or cleanup failures without `/dev/kvm`.
- [ ] **Step 3: Make the smallest repair** so acquired resources are released exactly once, partial construction closes what it acquired, and all close/unmap failures remain visible without masking the original error.
- [ ] **Step 4: Run targeted package tests** for every changed lifecycle path and confirm existing load bounds and I/O payload checks remain unchanged.

### Task 4: Document V1 operation and host requirements

**Files:**
- Create: `README.md`
- Modify only if needed for consistency: `docs/ARCHITECTURE.md`, `docs/DECISIONS.md`

**Interfaces:**
- README documents the claim, package boundaries, exact host requirements (`Linux`, `amd64`, usable `/dev/kvm` and hardware/permissions), command to run, and expected transcript.
- README lists consulted sources and explains that guest bytes are a documented raw x86 demo program; dependency claims match the code and `go.mod`. Correct stale architecture dependency text if the audit confirms it is inaccurate.

- [ ] **Step 1: Write the README** from current verified behavior and architecture; do not promise unsupported functionality.
- [ ] **Step 2: Check all commands and claims against the repository** and remove any claim not demonstrated by code or the M3 gate.

### Task 5: Run the final audit and reproducibility gate

**Files:**
- Review: all changed files, `go.mod`, `go.sum`, `README.md`, `docs/ARCHITECTURE.md`, and guest bytes in `internal/guest/program.go`.

**Interfaces:**
- No new interface; this task produces evidence for every M3/V1 gate.

- [ ] **Step 1: Run targeted tests, `go test ./...`, and `go vet ./...`** and record exact outcomes.
- [ ] **Step 2: From a fresh shell/repository state, run `go mod download`, `go test ./...`, `go vet ./...`, and `go run ./cmd/ironvm`** on a KVM-capable host; capture the transcript, verify all semantic acceptance items, and repeat the demo once.
- [ ] **Step 3: Review all `unsafe`, ABI widths/alignment, ioctl argument shapes, `kvm_run` offsets, memory bounds, and object lifetimes** against the Linux UAPI header cited by the repository; record findings and resolution.
- [ ] **Step 4: Audit dependencies and scope**: confirm no unintended module was added, guest bytes are documented, consulted sources are listed, and search for `MMIO`, `virtio`, `network`, `disk`, `ELF`, `paging`, `long mode`, and `multi-vCPU` additions.
- [ ] **Step 5: Confirm README architecture/host requirements and every M3 gate in `docs/PLAN.md`**, then prepare the final V1 commit only after all required evidence is green.
