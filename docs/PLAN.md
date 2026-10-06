# IronVM — Execution Plan

**Build window:** 4 hours  
**Primary language:** Go  
**Target:** Linux x86-64 + `/dev/kvm`  
**Method:** evidence-first, RED→GREEN, hard milestone gates  
**Modes:** (A) Codex + OMX agentic execution, and (B) manual learning/build execution

---

# 1. Definition of done

IronVM V1 is done only when:

```text
KVM API probe passes
        +
VM + 64 KiB guest RAM created
        +
one vCPU configured
        +
raw x86 guest actually executes
        +
guest emits "Hi" through port I/O
        +
host returns byte 42 through IN
        +
guest reports 42 through OUT
        +
guest reaches KVM_EXIT_HLT
        +
mandatory RED tests are GREEN
        +
go test ./... passes
        +
go vet ./... passes
        +
independent ABI/unsafe review passes
        +
fresh demo run reproduces expected transcript
        ↓
IRONVM V1
```

No Linux boot. No MMIO. No virtio. No networking. No second vCPU.

---

# 2. Operating rules

These apply to human and agentic execution.

## Rule 1 — Evidence before assumption

For unfamiliar KVM behavior:

```text
official KVM docs / Linux UAPI header
→ notes
→ hypothesis
→ command/test
→ observed result
→ implementation
```

Do not implement a remembered struct layout or magic constant without checking it.

## Rule 2 — RED before GREEN

Each part begins by expressing its completion claim as a test/probe that currently fails.

A valid RED failure demonstrates missing behavior, not a broken test harness.

## Rule 3 — Minimum change

Implement only enough to satisfy the current RED test.

## Rule 4 — Two attempts, then re-diagnose

A bug gets at most two code-fix attempts under one hypothesis.

```text
failure
→ inspect evidence
→ fix attempt 1
→ retest
→ fix attempt 2
→ retest
→ still failing: STOP editing
→ reopen docs/header/runtime state
→ form a different hypothesis
```

A required failure cannot be carried through a phase gate.

## Rule 5 — Hard phase boundary

At the end of every part:

```text
tests
→ review
→ evidence
→ gate verdict
→ STOP
```

Do not silently drift into the next part.

## Rule 6 — No self-certification

The implementer does not decide its own implementation is correct. Completion requires executable evidence and, in the agentic flow, independent verification.

---

# 3. Four-hour milestone map

```text
00:00 ───────────────────────────────────────────────────── 04:00

M0 / Part 0   Bootstrap + ABI reconnaissance      ~35 min
M1 / Part 1   VM + RAM + vCPU + HLT              ~65 min
M2 / Part 2   Bidirectional port-I/O protocol     ~65 min
M3 / Part 3   Hardening + trace + final audit     ~55 min
Buffer        Debug / README / recording          ~20 min
```

The clock controls scope. It is not permission to skip correctness gates.

If time pressure appears, remove optional polish before removing verification.

---

# 4. Milestone 0 — Bootstrap and ABI reconnaissance

## Objective

Prove the environment and freeze the minimal KVM ABI surface before writing the VMM.

## Learn first

Understand:

- what `/dev/kvm` represents;
- VM fd vs vCPU fd;
- `KVM_GET_API_VERSION`;
- `KVM_CREATE_VM`;
- how KVM ioctl encodings are represented;
- the minimum structs IronVM needs;
- host requirements for KVM access.

## Notes/evidence to capture

```text
host GOOS/GOARCH
kernel version
/dev/kvm presence + permissions
KVM API version expectation
exact UAPI header inspected
required ioctl names
required structure names
unknowns still open
```

## RED 0

Before implementing the KVM package, write a failing probe/test defining:

```text
OpenSystem()
→ succeeds on a KVM-capable host
→ APIVersion() == expected KVM API version
```

On a host without KVM, the probe must return a specific environment error.

Observe RED for the intended reason.

## GREEN 0

Implement only:

- open `/dev/kvm`;
- `KVM_GET_API_VERSION`;
- contextual error wrapping;
- clean close.

## Gate M0

```text
[ ] authoritative KVM docs/header read
[ ] required ABI surface listed
[ ] RED observed
[ ] API probe GREEN
[ ] environment error path explicit
[ ] targeted tests pass
[ ] no speculative VM abstraction added
```

**STOP.**

---

# 5. Milestone 1 — First hardware-virtualized guest: reach HLT

## Objective

Execute raw guest code and receive `KVM_EXIT_HLT`.

This is the first major proof.

## Learn first

Read and understand only:

```text
KVM_CREATE_VM
KVM_SET_USER_MEMORY_REGION
KVM_CREATE_VCPU
KVM_GET_VCPU_MMAP_SIZE
KVM_GET_SREGS
KVM_SET_SREGS
KVM_SET_REGS
KVM_RUN
KVM_EXIT_HLT
```

Understand:

- guest physical memory vs host virtual memory;
- why registered backing memory must remain alive;
- real-mode segment setup;
- why `RFLAGS` bit 1 is set;
- why `kvm_run` is mmap'd;
- fd ownership.

## RED 1

Create an integration test whose claim is:

```text
given a guest containing HLT
when vCPU 0 starts at RIP=0
then execution exits as KVM_EXIT_HLT
```

The test must fail before VM/vCPU implementation exists.

## GREEN 1 implementation order

### 1A — VM creation

Implement `System.NewVM()` only.

### 1B — Guest RAM

Allocate 64 KiB and register slot 0 at GPA 0. Add bounds checks for copying guest bytes.

### 1C — vCPU

Create vCPU 0, ask KVM for the vCPU mmap size, then map `kvm_run`. Never hard-code the mmap size.

### 1D — x86 state

Initialize only state necessary for a real-mode entry at address 0.

### 1E — Minimal run loop

Run until the first supported terminal exit and expose its reason.

## Verification

```bash
go test <targeted KVM integration test>
go test ./...
go vet ./...
```

Independent review focuses on:

- ABI struct size/alignment;
- mmap lifetime;
- guest RAM lifetime;
- register initialization;
- fd cleanup;
- unsafe pointer conversions.

## Gate M1

```text
[ ] RED 1 observed
[ ] raw HLT guest executes under KVM
[ ] KVM_EXIT_HLT observed
[ ] guest RAM registered safely
[ ] kvm_run size obtained from kernel
[ ] targeted + full tests GREEN
[ ] ABI/unsafe reviewer has no blocker
```

**STOP. Do not implement I/O before this gate passes.**

---

# 6. Milestone 2 — Bidirectional guest↔host port I/O

## Objective

Make the demo prove both directions:

```text
guest → host: "Hi"
host  → guest: 42
guest → host: 42
guest → host: HLT
```

## Learn first

Read the `KVM_EXIT_IO` contract and write down:

```text
direction
size
port
count
data_offset
where IN data must be written before re-entry
```

Confirm how `data_offset` relates to the mmap'd `kvm_run` region. Do not write this decoder from memory.

## RED 2A — Pure device tests

Specify before implementation:

```text
OUT 0xE9 'H' -> console buffer "H"
OUT 0xE9 'i' -> console buffer "Hi"
IN  0x5050    -> response byte 42
OUT 0xF4 42   -> result byte 42
unknown port  -> explicit error
```

Observe RED.

## GREEN 2A

Implement only the pure port device.

## RED 2B — Full integration transcript

Create the raw guest payload and an integration assertion:

```text
console == "Hi"
guest result == 42
final exit == HLT
I/O events occur in expected semantic order
```

Observe RED before the full exit dispatcher exists.

## GREEN 2B

Implement:

- KVM I/O exit decoder;
- safe `data_offset` bounds validation;
- OUT payload extraction;
- IN response writeback;
- run/resume loop;
- HLT completion.

## Gate M2

```text
[ ] pure device RED tests became GREEN
[ ] integration RED became GREEN
[ ] guest "Hi" received through KVM_EXIT_IO
[ ] host 42 returned to guest
[ ] guest reports 42 to host
[ ] final HLT observed
[ ] unknown ports fail loudly
[ ] I/O view cannot index outside mapped run memory
[ ] full suite GREEN
```

**STOP.**

---

# 7. Milestone 3 — Trace, hardening and proof

## Objective

Turn a working kernel experiment into a polished engineering artifact without adding new feature scope.

## RED 3 — Acceptance proof

Define semantic output assertions for:

```text
KVM API reported
VM/vCPU setup reported
console bytes visible
host input 42 visible
guest result 42 visible
HLT visible
status PASS
```

Do not over-test whitespace.

## GREEN 3A — Trace

Add minimal human-readable trace output. No external logger.

## GREEN 3B — Cleanup

Review/repair:

- fd closure;
- `munmap`;
- partial-construction cleanup;
- guest-memory lifetime;
- error paths.

## Audit A — Correctness

```text
targeted tests
go test ./...
go vet ./...
real KVM integration run
```

## Audit B — ABI / unsafe

Review:

```text
all unsafe usage
struct field widths
alignment assumptions
ioctl argument shapes
kvm_run offset arithmetic
memory bounds
lifetime assumptions
```

## Audit C — Dependency / provenance

Confirm:

```text
only intended dependencies exist
no copied VMM framework
no opaque generated KVM binding package
guest machine bytes are documented
sources consulted are listed in README
```

## Audit D — Scope

Search for accidental creep:

```text
MMIO
virtio
network
disk
ELF
paging
long mode
multi-vCPU
```

Remove unfinished accidental features.

## Audit E — Reproducibility

From a fresh shell/repo state:

```bash
go mod download
go test ./...
go vet ./...
go run ./cmd/ironvm
```

Capture the final transcript.

## Gate M3 / V1

```text
[ ] every mandatory RED test GREEN
[ ] deterministic transcript reproduced
[ ] no unresolved correctness blocker
[ ] no unresolved ABI/unsafe blocker
[ ] dependency/provenance audit clean
[ ] fresh-run reproduction succeeds
[ ] README explains architecture + host requirements
[ ] optional work explicitly post-V1
```

**COMMIT. STOP. V1 is finished.**

---

# 8. Codex + OMX agentic execution plan

## 8.1 Model routing

| Role | Model | Effort | Purpose |
|---|---|---:|---|
| Conductor / lead | `gpt-6.1-sol` | high | synthesis, phase gates, final decisions |
| Architect | `gpt-6.1-sol` | high | ABI boundaries, contracts |
| Critic | `gpt-6.1-sol` | high | adversarial challenge before implementation |
| Verifier | `gpt-6.1-sol` | medium/high | completion evidence |
| Code reviewer | `gpt-6.1-sol` | high | ABI/correctness/unsafe review |
| Explorer | `gpt-6-luna` | xhigh | repository/header mapping |
| Researcher | `gpt-6-luna` | xhigh | official KVM docs |
| Test engineer | `gpt-6-luna` | xhigh | RED tests and regressions |
| Executor | `gpt-6-luna` | xhigh | bounded implementation |
| Debugger | `gpt-6-luna` | xhigh | evidence-driven failure diagnosis |
| Writer | `gpt-6-luna` | xhigh | README and evidence summaries |

The root conductor is the engineering lead, not the default typist.

## 8.2 Native subagents vs OMX `$team`

### Native Codex subagents

Use for short, bounded work:

```text
inspect linux/kvm.h for exact fields
explain real-mode CPU state
research one ioctl
write/design one RED test
review one unsafe boundary
audit cleanup
verify one milestone
```

They return evidence to the conductor and terminate.

### OMX `$team`

Use only when there are durable, independently editable lanes, for example:

```text
worker A: internal/kvm VM/vCPU ABI
worker B: device + guest payload + pure tests
worker C: integration test / trace / docs
```

Do not create a team merely to search a few files or read the same header.

Preferred active concurrency: **2–5**. Keep the established practical ceiling at roughly **6–8** unless evidence justifies more.

---

# 9. Per-part OMX protocol

For every part, run the established controlled loop:

```text
1. EXPLORE
   Native explorers map only the relevant code/header/API surface.

2. RESEARCH
   Researchers resolve exact technical uncertainty from authoritative sources.

3. RED
   Test engineer writes a realistic failing test/probe first.
   Conductor confirms it fails for the intended reason.

4. ARCHITECT
   Sol architect freezes:
   - contract
   - invariants
   - exclusions
   - files allowed to change
   - acceptance criteria

5. CRITIC
   Sol critic attacks assumptions and the proposed contract.
   Resolve material objections before implementation.

6. GREEN
   Luna executor(s) implement the smallest change that can satisfy RED.
   Use OMX $team only for genuinely separate durable lanes.

7. TARGETED TESTS
   Run the smallest executable proof of the current claim.

8. FULL REGRESSION
   go test ./...
   go vet ./...
   plus KVM integration where required.

9. REVIEW
   Independent Sol reviewer checks correctness, ABI, unsafe use and scope.

10. VERIFY
    Independent Sol verifier checks acceptance criteria from evidence,
    not executor summaries.

11. EVIDENCE REPORT
    Conductor records:
    - RED observed
    - GREEN evidence
    - commands run
    - failures encountered
    - known limitations
    - files changed
    - gate verdict

12. COMMIT / STOP
    Commit only if gate passes.
    Stop before the next milestone.
```

Equivalent OMX skill flow when available:

```text
$deepsearch
→ $research
→ $plan / $ralplan
→ $tdd
→ $team 2–3:executor      # only for durable independent lanes
→ $code-review
→ $security-review        # ABI/unsafe/dependency boundary here
→ $ultraqa
→ evidence report
→ STOP
```

---

# 10. Recommended agent waves

## M0 — Reconnaissance only

```text
Conductor (6.1 Sol)
├─ Luna researcher: minimal KVM lifecycle + official docs
├─ Luna explorer: installed kvm.h constants/structs
├─ Luna test engineer: API-probe RED design
└─ Sol architect: synthesize ABI contract
```

No OMX team required.

Gate: independent Sol verifier.

## M1 — HLT execution

Reconnaissance:

```text
Luna A: memory-region registration
Luna B: vCPU mmap + KVM_RUN
Luna C: real-mode register setup
Luna D: integration RED test
```

Conductor synthesizes before edits.

Implementation: use a 2-worker OMX team only if useful.

```text
worker A: System / VM / guest-memory ABI
worker B: vCPU / run-region / real-mode path
```

Verification:

```text
Sol code reviewer: ABI + unsafe
Sol verifier: HLT acceptance proof
```

## M2 — Bidirectional port I/O

Reconnaissance:

```text
Luna A: KVM_EXIT_IO layout
Luna B: IN writeback semantics
Luna C: guest opcode/byte validation
Luna test engineer: device + transcript RED tests
```

Implementation lanes at most:

```text
worker A: exit decoder + run/resume loop
worker B: PortIO + guest program + tests
```

Verification:

```text
Sol reviewer: offsets, bounds, semantic ordering
Sol verifier: independent full transcript run
```

## M3 — Hardening

No broad implementation team.

```text
Sol reviewer A: ABI + unsafe
Sol verifier B: tests + integration transcript
Luna reviewer C: cleanup + dependency/provenance + docs
```

Conductor makes the final gate decision.

---

# 11. Codex prompt style

Keep prompts human-sized and phase-scoped.

## Start a phase

```text
Finish IronVM Milestone 1 only.

Follow PLAN.md exactly.
Before editing, parallelize bounded investigation of guest RAM registration,
vCPU mmap/KVM_RUN, real-mode register setup, and the RED integration test.
Use authoritative KVM docs and the host Linux UAPI header and return evidence
back to the conductor.

Then freeze the phase contract, ensure the HLT test is RED for the intended
reason, implement the minimum needed for GREEN, run targeted/full tests, and
perform independent ABI/unsafe review.

Do not begin Milestone 2.
Stop after the M1 gate report.
```

## Continue a phase

```text
Continue IronVM Milestone 1 from the current repository state.

Read PLAN.md and current evidence first.
Do not redo investigations already supported by evidence.
Resolve remaining M1 acceptance criteria only.
Apply the two-attempt debugging rule.
Stop at the M1 gate.
```

## Audit before advancing

```text
Audit IronVM Milestone 1 against PLAN.md.

Do not add features.
Use independent verification.
Fix only blockers required for M1.
Do not weaken RED tests.
Do not begin Milestone 2.
Report evidence and final gate verdict.
```

---

# 12. Agent failure discipline

## Duplicate work

If two agents investigate the same thing, keep the stronger evidence and cancel/reassign duplication. Do not spawn more agents to restate it.

## Conflicting conclusions

Do not vote.

```text
conflict
→ identify exact disputed fact
→ inspect authoritative docs/header or executable behavior
→ resolve from evidence
```

## Repeated implementation failure

After two failed fixes under one hypothesis:

```text
executor stops
→ debugger receives exact command/output/state
→ researcher rechecks docs/UAPI
→ architect updates hypothesis if needed
→ add a diagnostic RED probe if useful
→ implementation resumes only with new evidence
```

## Agent says "done"

Ignore the claim until independent verification exists.

---

# 13. Human/manual learning-and-build flow

This is the path when you write IronVM yourself.

The rule:

> Do not ask the model to manufacture code you do not understand. Use it to identify what to read, challenge your explanation, review narrow code you wrote, or diagnose evidence.

For each kernel operation, repeat this loop.

## A — Read

Read the authoritative docs/header for exactly one next operation.

Example:

```text
Goal: understand KVM_CREATE_VM.
Read:
- KVM API documentation for KVM_CREATE_VM
- corresponding definition in linux/kvm.h
```

Do not read the entire KVM subsystem first.

## B — Explain it yourself

Before code, answer:

```text
What object receives this ioctl?
What argument does it take?
What does success return/change?
What lifetime does the returned resource have?
What can fail?
What invariant must hold afterward?
```

If you cannot answer those, continue reading.

## C — Write concise notes

Example:

```text
KVM_CREATE_VM
- ioctl on /dev/kvm system fd
- returns VM fd on success
- V1 uses default machine type
- VM fd owns VM lifecycle
- next required operation: register guest RAM
```

Do not turn notes into a second project.

## D — Write RED

Before useful implementation, write the test/acceptance probe that proves the next claim.

Run it. Read why it failed.

Continue only if failure means "behavior not implemented yet," not "test is malformed."

## E — State the command's purpose

Before running a meaningful command, know what evidence it should produce.

Example:

```bash
go test ./internal/kvm -run TestAPIVersion -v
```

Purpose:

```text
prove the KVM system wrapper returns the kernel API version
```

Then execute it and read the full result.

## F — Implement one boundary

Write the smallest step yourself:

```text
open /dev/kvm
→ test
one ioctl helper
→ test
CreateVM
→ test
```

Do not write the whole VMM and debug it afterward.

## G — Explain the working path

When GREEN, trace:

```text
Go call
→ fd
→ ioctl/mmap
→ kernel KVM object/state
→ returned value/shared memory
→ Go interpretation
```

If you cannot explain it, the phase is not learned even if tests pass.

## H — Review

Before phase completion:

```bash
git diff
go test <targeted>
go test ./...
go vet ./...
```

For ABI changes, reopen the relevant `kvm.h` definitions and compare fields/types deliberately.

## I — Gate and stop

Check every phase criterion. Write `PASS` only with evidence. Commit. Only then read docs for the next milestone.

---

# 14. Manual command/evidence progression

Exact commands vary by distro; the learning sequence should resemble:

## Environment

```bash
go version
uname -a
test -e /dev/kvm && ls -l /dev/kvm
grep -n "KVM_API_VERSION" /usr/include/linux/kvm.h
grep -n "KVM_CREATE_VM" /usr/include/linux/kvm.h
```

Understand each result before continuing.

## Project bootstrap

```bash
mkdir ironvm
cd ironvm
go mod init <your-module-path>
```

Create only the files required for M0.

## Test loop

```bash
go test ./... -v
```

During RED/GREEN prefer targeted tests first:

```bash
go test ./internal/kvm -run TestAPIVersion -v
```

then regression:

```bash
go test ./...
go vet ./...
```

## ABI source inspection

```bash
grep -n "struct kvm_userspace_memory_region" /usr/include/linux/kvm.h
grep -n "struct kvm_regs" /usr/include/linux/kvm.h
grep -n "struct kvm_sregs" /usr/include/linux/kvm.h
grep -n "struct kvm_run" /usr/include/linux/kvm.h
```

Open enough surrounding lines to understand the whole relevant definition.

---

# 15. RED test ledger

| ID | Milestone | RED claim | GREEN proof |
|---|---|---|---|
| R0 | M0 | KVM system probe unavailable/unimplemented | API version read correctly |
| R1 | M1 | minimal guest cannot execute to HLT | `KVM_EXIT_HLT` observed |
| R2A | M2 | device protocol unimplemented | pure port-device tests pass |
| R2B | M2 | guest↔host transcript unsupported | `Hi`, IN 42, OUT 42, HLT |
| R3 | M3 | final acceptance proof incomplete | deterministic end-to-end transcript |

Never delete or weaken a RED test because the implementation struggles with it.

---

# 16. Evidence record per milestone

At each gate record:

```text
MILESTONE:
DATE/TIME:

RED:
- test:
- expected failure:
- observed failure:

GREEN:
- implementation summary:
- targeted command:
- result:

REGRESSION:
- go test ./...:
- go vet ./...:
- integration:

REVIEW:
- reviewer:
- blockers:
- resolved:

FILES CHANGED:
- ...

KNOWN LIMITATIONS:
- ...

GATE:
PASS / FAIL
```

Under OMX, the conductor may keep this in the normal evidence/result artifact. A four-hour project does not need a forest of permanent process files.

---

# 17. Git progression

One main feature branch is enough unless OMX uses temporary worktrees internally.

Commit progression should tell the architecture story:

```text
chore: bootstrap ironvm and KVM probe
feat: execute minimal guest to KVM_EXIT_HLT
feat: handle bidirectional guest port I/O
test: harden KVM ABI and exit handling
docs: document IronVM architecture and demo
```

Do not create one long-lived branch per agent.

---

# 18. What to cut first if time runs short

Cut in this order:

```text
1. fancy trace formatting
2. optional assembly source/generator
3. extra test cosmetics
4. README polish
```

Do **not** cut:

```text
HLT proof
bidirectional I/O proof
RED tests
bounds checks on KVM I/O data
ABI/unsafe review
full regression
```

---

# 19. Post-V1 only

After V1, possible extensions include:

- `KVM_EXIT_MMIO`;
- CPUID configuration;
- protected/long mode;
- ELF guest loader;
- second vCPU;
- IRQ injection;
- serial/UART emulation;
- virtio;
- tiny Linux boot;
- snapshots;
- tracing/perf experiments.

None may enter V1 unless a current mandatory acceptance criterion proves impossible without it.

---

# 20. Final build rule

The project is successful when you can explain every important line at the kernel boundary.

Not:

```text
prompt model
→ receive VMM
→ run it
```

Instead:

```text
read
→ understand
→ note
→ define RED
→ execute an evidence-producing command
→ inspect result
→ implement the minimum
→ make RED GREEN
→ review
→ independently verify
→ gate
→ continue
```

That is the build.
