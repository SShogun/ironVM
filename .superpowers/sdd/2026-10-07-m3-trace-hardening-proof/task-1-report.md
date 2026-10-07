# Task 1 Report: End-to-end transcript assertion

## Status

Complete. Added `cmd/ironvm/main_linux_amd64_test.go` with `TestDemoTranscript`.

## Implementation

The test invokes `go run .` with its working directory inherited from the `cmd/ironvm` package test process, captures combined stdout/stderr, and checks for all eight requested semantic transcript fragments without asserting whitespace.

## RED evidence

Command:

```text
go test ./cmd/ironvm -run TestDemoTranscript -count=1
```

Result: exit status 1, as expected for the current CLI transcript. The host has accessible KVM: the subprocess succeeded and printed `KVM API: 12`, so this was not skipped.

Output:

```text
--- FAIL: TestDemoTranscript (0.10s)
    main_linux_amd64_test.go:28: transcript missing "guest RAM: 65536 bytes"
        transcript:
        KVM API: 12
    main_linux_amd64_test.go:28: transcript missing "vCPU: 0"
        transcript:
        KVM API: 12
    main_linux_amd64_test.go:28: transcript missing "guest console: Hi"
        transcript:
        transcript:
        KVM API: 12
    main_linux_amd64_test.go:28: transcript missing "host input: 42"
        transcript:
        KVM API: 12
    main_linux_amd64_test.go:28: transcript missing "guest result: 42"
        transcript:
        KVM API: 12
    main_linux_amd64_test.go:28: transcript missing "HLT"
        transcript:
        KVM API: 12
    main_linux_amd64_test.go:28: transcript missing "status: PASS"
        transcript:
        KVM API: 12
FAIL
FAIL	ironvm/cmd/ironvm	0.100s
FAIL
```

The expected RED is specifically the missing transcript behavior: the existing executable prints its KVM API but omits the remaining seven required semantic fragments.

## Scope

Only the requested test file was staged/committed. Pre-existing untracked `AGENTS_V2_WARHOST.md` and `docs/superpowers/plans/` were left untouched.

## Fix round 1: narrowly scoped KVM environment skip

### Files changed

- `cmd/ironvm/main_linux_amd64_test.go`: on `go run .` failure, skip only if combined output contains an `open /dev/kvm:` error with `no such file or directory` or `permission denied`. Added table-driven helper coverage for these two cases plus API mismatch, VM setup permission failure, and unrelated missing-file command failure. The original eight transcript assertions remain unchanged.
- This report: appended this fix-round record.

### Commands and results

`go test ./cmd/ironvm -run '^TestIsUnavailableKVMError$' -count=1`

```text
ok  ironvm/cmd/ironvm  0.002s
```

`go test ./cmd/ironvm -run '^TestDemoTranscript$' -count=1`

Exited 1 as expected on this KVM-capable host. `go run .` completed and printed `KVM API: 12`; the test failed only because the current executable omits each of the remaining seven required transcript fragments (guest RAM, vCPU, console, host input, guest result, HLT, and PASS). It did not skip.

### Self-review

- The skip predicate requires both the `/dev/kvm` open operation and one of the two recognized OS error messages. Generic “permission denied,” API mismatch, and unrelated command errors remain fatal.
- The subprocess still runs through `go run .` from the package working directory, and all semantic transcript expectations are preserved.
- Only the specified integration test file and this report were changed in this round. The existing untracked `AGENTS_V2_WARHOST.md` was left untouched.
