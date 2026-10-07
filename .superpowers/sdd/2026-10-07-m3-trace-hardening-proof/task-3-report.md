# M3 Task 3 — KVM resource lifecycle audit and repairs

Status: complete. Two failed-unmap cleanup defects repaired; ownership audit, RED/GREEN evidence, and final verification recorded below.

## Scope and files

Reviewed `internal/kvm/system.go`, `system_linux_amd64.go`, `vm_linux_amd64.go`, `vcpu_linux_amd64.go`, `internal/vmm/run.go`, the CLI's outer System cleanup, and existing KVM/VMM integration and I/O tests. Modified only VM and vCPU cleanup/state handling and added `internal/kvm/lifecycle_linux_amd64_test.go`. Constructors, the System implementation, and RunDemo required no resource ownership repair.

Two confirmed defects received focused failing tests before repair:

1. VM.Close retained a guest mapping after Munmap failed, but had already set `closed`, so every further Close returned `errVMClosed` and could not release the mapping.
2. VCPU.Close cleared `run` even if Munmap failed and had already set `closed`, losing the owned mapping and preventing another cleanup attempt.

The parent task clarified the teardown contract: the first VM.Close attempt must stop LoadGuest/NewVCPU even when slot removal fails; keep fd/slot/RAM available for another Close; do not mark fully closed until resources are released. The equivalent vCPU execution guard applies while its mapping cleanup is pending. This narrowly changes failed-Close behavior, while successful Close, nil/duplicate error behavior, public constructor signatures, and contextual error wrapping remain intact.

## Acquisition and release inventory

| Owner/resource | Acquisition | Release and failure handling |
| --- | --- | --- |
| System `/dev/kvm` fd | OpenSystem: syscall.Open with O_RDWR and O_CLOEXEC | System.Close attempts Close once. Closed state prevents another fd Close even after an error. API query failure or API mismatch uses closeAfterFailure, which joins a Close failure with the original cause. |
| VM fd | System.NewVM: KVM_CREATE_VM on the System fd | Partial construction uses cleanupVMConstruction. Fully constructed VM.Close first removes slot 0, then attempts fd Close once. A Close error is returned and never triggers an fd retry. |
| VM guest RAM mmap | NewVM: 64 KiB anonymous, private, read/write mmap | Constructor failure closes the VM fd while RAM is still mapped, then Munmap. VM.Close removes slot 0 before any guest unmap. Failed Munmap retains `memory` for a later Close attempt. |
| VM slot 0 (kernel reference to RAM) | NewVM: KVM_SET_USER_MEMORY_REGION at GPA 0, mapping address and length | VM.Close issues KVM_SET_USER_MEMORY_REGION with slot 0 and zero memorySize. Failed removal retains fd and RAM and stops execution-facing VM operations. A successful removal is not repeated when only guest unmapping remains. |
| vCPU fd | VM.NewVCPU: KVM_CREATE_VCPU | If the subsequent kvm_run mmap fails, close the acquired fd and join any close error with the mmap cause. VCPU.Close attempts fd Close once after its first unmap attempt, even when that unmap fails. |
| vCPU kvm_run mmap | NewVCPU: read/write MAP_SHARED mmap of the kernel-provided size on the vCPU fd | VCPU.Close unmaps before fd Close. Failed unmapping retains `run`; another Close retries that mapping only. Successful unmapping clears the slice. |
| RunDemo VM and vCPU ownership | Takes ownership of successful NewVM/NewVCPU results; does not own the supplied System | VM defer is registered immediately after NewVM, vCPU defer immediately after NewVCPU. LIFO cleanup runs vCPU.Close before VM.Close. |

Guest memory remains owned by VM.memory throughout its registered slot lifetime. NewVM keeps both the memory region and backing memory alive across the registration ioctl. Slot removal keeps the region alive across its ioctl. No live vCPU exists on any NewVM failure path, so closing the construction-time VM fd before unmapping destroys the VM's slot while the backing memory is still valid. On completed VMs, explicit slot removal remains necessary because a vCPU fd or mapping may retain a VM reference.

## Deferred cleanup order

Successful RunDemo acquisition order is System (owned by caller), VM, guest RAM/slot, vCPU, kvm_run. RunDemo release order is:

1. vCPU kvm_run unmap attempt.
2. vCPU fd Close attempt, including when its unmap failed.
3. VM slot 0 removal; keep VM fd and guest RAM if removal fails.
4. VM fd Close attempt after successful removal.
5. Guest RAM unmap attempt.
6. The CLI caller's System.Close after RunDemo returns.

The VM/vCPU defers join cleanup errors into named `retErr`, preserving any guest execution, device dispatch, validation, or trace-output error. No error is replaced by a deferred cleanup result. RunDemo's ordering and joins were already correct and were left unchanged.

## Failure paths checked

### System — no repair needed

- Open failure: no fd was acquired; returns EnvironmentError with the original cause.
- API ioctl failure: close acquired fd, preserving the EnvironmentError and ioctl cause.
- API mismatch: close acquired fd and preserve the mismatch error.
- Failure cleanup Close failure: errors.Join preserves original and cleanup causes; covered with real EBADF and a sentinel cause in TestConstructionCleanupPreservesAllErrors.
- Normal Close or Close failure: marks the fd consumed before the syscall, reports a contextual error, and rejects duplicate Close. Retrying a Linux Close after an error can close a reused fd, so no retry was added.

### VM construction — no repair needed

- Nil/closed System: validation before fd acquisition.
- KVM_CREATE_VM failure: returns contextual errno before RAM mmap or owned VM state exists.
- Guest RAM mmap failure: close acquired VM fd; no mapping exists to unmap.
- Memory registration failure: close VM fd while RAM is still live, then unmap.
- KVM_GET_VCPU_MMAP_SIZE failure, zero size, or size exceeding int capacity: close VM fd (no vCPU has been created), then unmap live RAM.
- cleanupVMConstruction attempts unmap even after fd Close reports failure and joins both cleanup causes with the original constructor error. TestConstructionCleanupPreservesAllErrors exercises real EBADF/EINVAL without /dev/kvm.

### VM Close — repaired

- Nil VM and fully closed VM retain their existing errors.
- First Close sets `closing`; LoadGuest and NewVCPU reject both closing and closed VMs.
- Failed slot removal: return its error without closing fd or unmapping RAM; retain all resources needed to retry removal. The object is not marked fully closed.
- Successful slot removal: mark fd release attempted before Close. Slot removal and fd Close never repeat after this point, including when fd Close reports an error.
- Failed guest Munmap: join error with any fd Close failure, retain mapping, leave fully closed false. Further Close calls retry only guest Munmap.
- Successful guest Munmap: clear memory and mark fully closed; duplicate Close returns errVMClosed without side effects.

### vCPU — repaired Close; no construction repair needed

- Nil VM, negative id, closing/closed VM, invalid mmap size: reject before acquisition.
- KVM_CREATE_VCPU failure: returns errno without an acquired vCPU fd or mapping.
- kvm_run mmap failure: close acquired fd once; join a close failure with mmap cause under the existing contextual error.
- First VCPU.Close sets `closing`, unmaps, then closes fd once regardless of unmap outcome. Run, ConfigureRealMode, and CompleteIO return their existing contextual closed errors while cleanup is pending.
- Failed run Munmap: retain original `run`, preserve unmap and close errors, and leave fully closed false.
- Further Close: retry only retained mapping; never retry fd Close.
- Successful unmap: clear run, mark fully closed; duplicate Close remains an error.

### RunDemo — no repair needed

- Nil inputs: reject before resource acquisition.
- NewVM failure: constructor owns its partial cleanup; no RunDemo defer is required yet.
- LoadGuest failure: VM defer is already present.
- NewVCPU failure: its constructor cleans any partial fd, then the VM defer runs.
- Configuration, setup/entry/I/O/HLT/summary trace failures, KVM_RUN/decode failures, device failures, unsupported exits, width/count/payload validation, and protocol result failures: both defers are already present and execute in vCPU-before-VM order.
- Existing live KVM trace-error tests exercise setup, entry, OUT, IN, HLT, and summary failure boundaries; the exact transcript and HLT tests also passed.

## Focused RED/GREEN evidence

Used unexported per-call cleanup helpers `VM.closeWith` and `VCPU.closeWith`. Public Close methods supply the real slot-removal/Close/Munmap functions. No exported injection interface, global mutable syscall hooks, constructor seams, or general resource abstraction was added. The initial seam extraction did not alter cleanup behavior; the regression tests then failed against that original behavior.

Tests acquire real anonymous mmaps and /dev/null fds, inject only the needed failure boundaries, and eventually call the real Close/Munmap. Their expected release sequence is literal. They check original and cleanup errors with errors.Is, retained ownership, fully closed state, duplicate-close behavior, and execution rejection. Fallback test cleanup releases remaining test resources without attempting an already released fd again.

Initial RED command:

```text
$ go test ./internal/kvm -run 'Test(VM|VCPU)CloseRetriesFailedUnmap' -v
=== RUN   TestVMCloseRetriesFailedUnmap
    lifecycle_linux_amd64_test.go:56: retry Close error = KVM VM is already closed, want retained unmap failure
--- FAIL: TestVMCloseRetriesFailedUnmap (0.00s)
=== RUN   TestVCPUCloseRetriesFailedUnmap
    lifecycle_linux_amd64_test.go:99: failed unmap lost run mapping
    lifecycle_linux_amd64_test.go:110: retry Close error = vCPU is already closed, want retained unmap failure
--- FAIL: TestVCPUCloseRetriesFailedUnmap (0.00s)
FAIL
FAIL    ironvm/internal/kvm    0.002s
```

After the mapping-retention/retry repair, both tests passed. The clarified terminal teardown requirement then received a second RED run:

```text
$ go test ./internal/kvm -run 'Test(VM|VCPU)Close' -v
--- FAIL: TestVMCloseRetriesFailedUnmap
    VM marked fully closed while guest unmap is pending
--- FAIL: TestVCPUCloseRetriesFailedUnmap
    vCPU marked fully closed while run unmap is pending
--- FAIL: TestVMCloseSlotRemovalFailureRetainsResourcesAndStopsOperations
    LoadGuest during teardown = <nil>, want closed
    NewVCPU during teardown = create vCPU 0 (KVM_CREATE_VCPU): inappropriate ioctl for device, want closed
FAIL
```

After adding the explicit closing state and separating fd release from fully closed state, the three lifecycle tests passed. Assertions were then tightened to require the contextual closed error, so an accidental ioctl attempt returning ENOTTY cannot satisfy an execution-rejection test.

Final focused GREEN:

```text
$ go test ./internal/kvm -run 'Test(VM|VCPU)Close' -v
=== RUN   TestVMCloseRetriesFailedUnmap
--- PASS: TestVMCloseRetriesFailedUnmap (0.00s)
=== RUN   TestVCPUCloseRetriesFailedUnmap
--- PASS: TestVCPUCloseRetriesFailedUnmap (0.00s)
=== RUN   TestVMCloseSlotRemovalFailureRetainsResourcesAndStopsOperations
--- PASS: TestVMCloseSlotRemovalFailureRetainsResourcesAndStopsOperations (0.00s)
PASS
ok      ironvm/internal/kvm    0.002s
```

## Bounds and I/O verification

LoadGuest's existing subtraction-based bounds calculation is unchanged. TestVMLoadGuestBounds covers whole-RAM load, final byte, empty load exactly at the end, nonempty load at end, GPA past RAM, maximum uint64 GPA, code larger than RAM, and code overrunning the remaining tail. Rejected operations leave guest bytes unchanged.

The existing decodeExit, ioPayloadRange, CompleteIO payload/pending-exit checks, and RunDemo's byte-wide/count-one I/O validation are unchanged. Existing tests passed for OUT data copies, IN completion, payload beyond mapping, metadata overlap, zero size/count, maximum offset, exact response length, repeated completion rejection, and caller-mutated exit metadata rejection.

`go test ./internal/kvm ./internal/vmm -v` passed all tests and subtests, including live KVM integration tests with no skips:

```text
PASS
ok      ironvm/internal/kvm    0.020s
PASS
ok      ironvm/internal/vmm    0.151s
```

## Full-suite result and observed interruption

The first full-suite run failed in an unchanged constructor before any modified cleanup/state path:

```text
$ go test ./...
ok      ironvm/cmd/ironvm      0.279s
ok      ironvm/internal/device (cached)
?       ironvm/internal/guest  [no test files]
ok      ironvm/internal/kvm    0.016s
ok      ironvm/internal/trace  (cached)
--- FAIL: TestRunDemoPropagatesTraceErrors (0.14s)
    --- FAIL: TestRunDemoPropagatesTraceErrors/HLT (0.03s)
        integration_linux_amd64_test.go:104: RunDemo() error = run demo: create VM: ioctl KVM_CREATE_VM: interrupted system call, want trace output failed
FAIL
FAIL    ironvm/internal/vmm    0.170s
FAIL
```

A single fresh rerun passed without another EINTR:

```text
$ go test ./... -count=1
ok      ironvm/cmd/ironvm      0.239s
ok      ironvm/internal/device 0.002s
?       ironvm/internal/guest  [no test files]
ok      ironvm/internal/kvm    0.022s
ok      ironvm/internal/trace  0.003s
ok      ironvm/internal/vmm    0.224s
```

The package-level live KVM run had also passed all trace-error cases. The diff does not change KVM_CREATE_VM, so this one observed interruption is recorded as a test-environment concern; ioctl retry policy was not broadened as part of the cleanup repair.

## Self-review

- Constructors and ownership transfers are unchanged; no speculative API or abstraction changes.
- Original cleanup ordering is retained. RAM cannot be unmapped before slot removal succeeds; vCPU cleanup still precedes VM cleanup in RunDemo.
- `closing` disables execution-facing operations while failed cleanup remains retryable. `closed` means no owned mapping cleanup remains. VM `fdReleased` records the one fd Close attempt after slot removal.
- Failed unmaps retain their original slices. A retry does not operate on a released fd or repeat successful slot removal.
- Close and unmap failures remain joined on the first attempt, and a repeated failed unmap remains visible on retry. A later successful retry does not repeat a previously reported close error.
- Local helpers have no mutable global state and are used by the public Close implementation; tests exercise the same cleanup logic rather than replacing Close itself.
- Tests detect removing retention, removing retry support, repeating fd release/slot removal, allowing operations during teardown, premature closed state, and masking either cleanup error.
- The added LoadGuest and constructor error-preservation tests characterize unchanged behavior; no unnecessary production repair was made to those paths.
- `git diff --check` passed.
- Untracked `AGENTS_V2_WARHOST.md` was not modified or included.

## Limits and concerns

OS cleanup failures remain errors; retry support does not guarantee that a persistently failing syscall will eventually succeed. Constructors and RunDemo perform best-effort cleanup and report failures; they do not add automatic retry loops or transfer partial resources through new public interfaces. The only observed suite concern was the single KVM_CREATE_VM EINTR above; its fresh rerun passed. No known concern remains in the modified ownership/state paths.

## Final verification

After the assertion tightening and final self-review, a fresh suite run again passed without EINTR:

```text
$ go test ./... -count=1
ok      ironvm/cmd/ironvm      0.265s
ok      ironvm/internal/device 0.002s
?       ironvm/internal/guest  [no test files]
ok      ironvm/internal/kvm    0.020s
ok      ironvm/internal/trace  0.002s
ok      ironvm/internal/vmm    0.158s
```

`git diff --check` passed. The task commit contains only the VM/vCPU source changes, focused lifecycle tests, and this required report. This report is force-added because the local ignore rules ignore `.superpowers` reports; the existing Task 1 report is already tracked.
