# ExecPlans

This document defines what an **ExecPlan** is in this repository: its purpose,
its non-negotiable requirements, its required sections, and the writing
guidelines that keep it usable. It is a meta-document. It contains no plan of
its own.

An ExecPlan is a living design document that carries a complex change from
design through implementation to retrospective. It exists because long tasks
lose their context: a session ends, a machine reboots, an encode runs for two
hours, a reviewer arrives a week later. The plan is the part that survives.

## When to write one

Write an ExecPlan for work that a careful engineer could not hold in one
sitting:

- Changes that touch the safety gates in `internal/processor/policy.go`,
  `internal/media/media.go` or `internal/quality/selector.go`, where being
  wrong means silently degrading or destroying a user's source media.
- Changes that cross package boundaries (for example, threading a new
  configuration flag from `internal/config` through `internal/processor` into
  `internal/ffmpeg`).
- Anything that needs real FFmpeg/libx265 runs, multi-platform verification, or
  hours of encoding to validate.
- Refactors of the per-file orchestration in `internal/processor/processor.go`.

Do not write one for a typo, a single-line fix, a comment, or a change whose
whole verification is `go test ./...`. For those, just do the work.

## Where plans live

Plans are Markdown files under `.agent/plans/`, named
`YYYY-MM-DD-short-slug.md` — for example
`.agent/plans/2026-08-17-av1-encoder-support.md`. Start from
`.agent/plans/TEMPLATE.md`. Plans are committed to the repository: they are
project history, not scratch files.

One plan per problem. If a plan grows a second, independent problem, split it
and cross-reference both files by their repo-relative paths.

## Format

An ExecPlan is a Markdown file whose content *is only* the plan. Write it
directly as Markdown — do not wrap the whole file in triple backticks. Use
fenced code blocks only for commands, transcripts, diffs and code, and never
nest triple backticks inside a fenced block; indent instead when you need a
block inside a block.

When an ExecPlan is delivered inside a chat message rather than a file, wrap it
in a single ` ```md ` fenced block so it arrives as one copyable unit.

## Non-negotiable requirements

These five are absolute. A document that fails any of them is not an ExecPlan.

1. **It is completely self-contained.** Every piece of knowledge needed to
   succeed is in the document. Do not write "see the FFmpeg documentation" or
   "as discussed above in the issue" — state the flag, the semantics, the
   observed behaviour, inline.
2. **It is a living document.** Update it as you go: check off progress with a
   timestamp, record what surprised you, log every decision you made and why.
   A plan that still reads exactly as it did before implementation started is a
   plan that was not used.
3. **A complete newcomer can take it end to end.** Assume a competent engineer
   with no prior exposure to this repository, this codec, or this conversation.
   They must be able to finish the work from the document alone.
4. **It produces empirically working behaviour.** The goal is observed,
   verified behaviour — a real encode that validated, a real test that passed
   — not code that merely matches a definition. State what you actually ran and
   what actually came back.
5. **It defines all of its terms.** Spell out jargon in plain language on first
   use: CRF, SSIM, PQ/HLG, Dolby Vision, atomic rename, generational loss.
   Assume the reader knows Go and nothing about video.

## Required sections

Every ExecPlan has all of the following, in this order.

### Purpose and big picture

What changes for the person running `vcompress`, in their terms. Why the work
is worth doing. What is deliberately out of scope.

### Context and orientation

The background a newcomer needs: which packages are involved and what each one
is responsible for, the repo-relative paths of every file that matters, the
current behaviour before the change, and definitions of the terms used
throughout. Name files as full repo-relative paths, for example
`internal/quality/selector.go`, never "the selector".

### Interfaces and dependencies

The concrete contracts: exported function signatures being added or changed,
struct fields, configuration flags with their defaults and validation rules,
new FFmpeg/ffprobe arguments and what they mean, and any external tool version
requirements. Be exact enough that two people implementing from this section
would produce compatible code.

### Plan of work

Prose that walks through the change in the order it should be made, explaining
why that order is correct — what must land before what, and what would break if
the order were reversed. This is the narrative section; it carries the design
reasoning.

### Concrete steps

The exact commands to run and the working directory to run them in. When a
command produces meaningful output, include a short expected transcript so the
reader can compare against reality. Prefer this repository's mise tasks, which
mirror CI:

```bash
mise run build          # go build -o vcompress ./cmd/vcompress
mise run test           # go test ./...
mise run vet            # go vet ./...
mise run check          # test + vet, as CI runs them
mise run build-windows  # GOOS=windows GOARCH=amd64 build
mise run test-integration
```

### Validation and acceptance

How anyone can prove the change works, and what "done" means in observable
terms. For this repository that normally includes:

- `mise run check` — unit tests and `go vet` on all packages.
- `mise run build-windows` — the Windows path in `internal/fsutil` must keep
  compiling from any host OS.
- `mise run test-integration` — the real FFmpeg/libx265 round trip in
  `internal/processor/integration_test.go`, which needs `ffmpeg` and `ffprobe`
  with `libx265` on `PATH`.
- A `--dry-run` pass over a directory of real files, when discovery, policy or
  selection behaviour changed, with the relevant lines of
  `ffmpeg-compress.log` quoted as evidence.
- An explicit statement that the safety policy in `README.md` still holds:
  sources are only ever replaced after structural validation, the full decode
  check and the minimum savings gate pass.

State expected numbers where you can ("expect 41 tests passed", "expect the
output to be at least 5% smaller"), so a mismatch is visible.

### Idempotence and recovery

What happens when a step is re-run, and how to get back to a known-good state.
Note anything that is not safely repeatable. This matters here because runs
touch real files: say where temporary outputs land
(`.<name>.ffmpeg-compressing-*` beside the source), how an interrupted run is
cleaned up, and how to undo a partial change.

### Progress

Fine-grained checkboxes, each small enough to finish in one sitting, each
stamped with a UTC timestamp when completed:

```text
- [x] (2026-08-17 13:00Z) Added `--target-codec` parsing and validation in internal/config/config.go.
- [ ] Thread the selected target codec into internal/ffmpeg/client.go.
```

This is the one section where a list is the right form. Keep it current — it is
how the next session, or the next person, knows where to resume.

### Surprises and discoveries

Anything that did not behave as the plan assumed: an ffprobe field that is
absent for some containers, an x265 log line whose format differs by version, a
platform difference in rename semantics, an existing bug you tripped over.
Quote the actual output. This section is often the most valuable part of the
document.

### Decision log

Every decision worth defending, as *decision, rationale, date and author*:

```text
2026-08-17 — jun: Reject AV1 output for now. libaom is too slow for the
sequential single-encode design, and libsvtav1 is not present in the Ubuntu
runner's FFmpeg build, so CI could not validate it.
```

Include the alternatives you rejected and why. A future reader must be able to
tell a deliberate choice from an accident.

### Artifacts and notes

The evidence: command transcripts, relevant `ffmpeg-compress.log` excerpts,
`ffprobe` output before and after, diffs, measured file sizes and SSIM values.
Trim to what supports a claim.

### Outcomes and retrospective

Written at the end: what actually shipped, what is still open, what you would
do differently. If the plan diverged from its original design, say where and
why.

## Writing guidelines

- **Prefer plain prose.** Sentences and paragraphs, not bullet lists, tables or
  long enumerations. The Progress section is the exception. Prose forces the
  reasoning to be explicit; a bullet list lets it hide.
- **Show evidence in quotes.** Quote real observations and real output rather
  than describing them. "ffprobe reports `color_transfer` as absent, not
  `unknown`, for this file" beats "ffprobe may not report the transfer".
- **Never point outward for required knowledge.** No links as a substitute for
  content. If an external fact is needed, restate it in the plan, with enough
  context that it stands alone.
- **Use full repo-relative paths.** `internal/fsutil/replace_windows.go`, not
  "the Windows replace file".
- **Prototyping milestones are encouraged.** A spike that explores two
  approaches in parallel and is then consolidated is a legitimate step; say so
  explicitly, say how the comparison will be judged, and say what happens to
  the losing branch.
- **Revise the whole document, not just the tail.** When the design changes,
  update every affected section — including Progress — and record the reason for
  the revision in the Decision log. The plan must read correctly top to bottom
  at all times, because the next contributor will read it that way.
- **Respect this project's safety posture.** vcompress exists to avoid
  destroying people's video. Any plan that relaxes a validation gate, a skip
  rule or the replacement path must say so in its own section, explain the
  risk, and describe how the change was proven safe.
