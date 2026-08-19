# Add a keep-original output mode

## Purpose and big picture

Add an opt-in `--keep-original` command-line option. A normal run continues to
replace or remove the source only after all existing quality, structural,
decode, and size checks pass. With the new option, a validated HEVC output is
published beside the source and the source path and bytes are left unchanged.
For a source whose container can already be retained, such as `movie.mp4`, the
new output is `movie.hevc.mp4`. For a source that is already remuxed by the
existing policy, such as `movie.avi`, the output remains `movie.mkv`. Existing
destination files are never overwritten. Changing codec eligibility, quality
thresholds, validation, encoding, or metadata preservation is out of scope.

## Context and orientation

`cmd/vcompress/main.go` defines CLI flags, prepares configuration, and reports
aggregate results. `internal/config/config.go` owns runtime configuration and
defaults. `internal/fsutil/paths.go` chooses a final path and creates a unique
same-directory temporary path. `internal/processor/processor.go` performs CRF
selection, full encoding, validation, the minimum-savings check, and final
publication. Its current same-container publication replaces the source using
the operating-system-specific `internal/fsutil/replace_unix.go` or
`internal/fsutil/replace_windows.go`; a changed-container output is renamed to
the final path before the source is removed. `internal/processor/processor_test.go`
and `internal/fsutil/paths_test.go` exercise those decisions, while
`internal/processor/integration_test.go` performs a real FFmpeg/libx265 round
trip. `README.md` documents the CLI and safety policy.

HEVC is the output video codec produced by x265. A container is the outer file
format, such as MP4, MOV, MKV, or AVI. Publication means moving the fully
validated same-directory temporary output to its user-visible final path. An
atomic rename makes the new path visible as one filesystem operation. The
minimum-savings gate rejects output whose byte-size reduction is below the
configured threshold. The new option changes only publication: all gates run
before any output becomes visible, exactly as they do now.

## Interfaces and dependencies

`config.Config` gains `KeepOriginal bool`, defaulting to `false` so existing
invocations keep their behavior. `cmd/vcompress/main.go` binds it to
`--keep-original` with no argument. `fsutil.PlanOutput` changes from
`PlanOutput(input string) (OutputPaths, error)` to
`PlanOutput(input string, keepOriginal bool) (OutputPaths, error)`. When
`keepOriginal` is true and the lower-cased extension is `.mp4`, `.m4v`, `.mov`,
or `.mkv`, `Final` is `<stem>.hevc<original-extension>`; for other extensions,
`Final` remains `<stem>.mkv`. Temporary outputs remain
`.<stem>.ffmpeg-compressing-*<output-extension>` beside the source.

No new library, FFmpeg argument, ffprobe argument, or external version
requirement is introduced. A retained-original conversion reports zero
`SavedBytes`, because keeping both files does not reduce occupied disk space;
the per-file log reports the validated output reduction as a potential saving.

## Plan of work

First add the configuration field and CLI binding so publication behavior has
one explicit input and the default remains compatible. Then extend output-path
planning and its unit tests, because the processor must know a non-conflicting
destination before encoding. Next thread the option through the processor,
retain the source after all gates pass, and add tests that compare the original
bytes and verify the side-by-side output. Update startup and completion logs so
the selected behavior and disk-space accounting are unambiguous. Finally
document the option and verify unit, static-analysis, Windows compile, and real
FFmpeg paths.

The existing validation order is preserved: encode to the unique temporary
path, probe and fully decode it, enforce the size threshold, apply source mode
and timestamps to the output, then publish it. No source deletion or replacement
occurs in keep-original mode.

## Concrete steps

Run all commands from `/Users/jun_networks/workspace/vcompress`.

```bash
gofmt -w cmd/vcompress/main.go internal/config/config.go internal/fsutil/paths.go internal/fsutil/paths_test.go internal/processor/processor.go internal/processor/processor_test.go internal/processor/integration_test.go
mise run check
mise run build-windows
mise run test-integration
```

`mise run check` should finish with all Go package tests passing and no `go
vet` diagnostics. `mise run build-windows` should exit zero. The integration
test should report passing subtests for default replacement and retained-source
publication, provided FFmpeg, ffprobe, and libx265 are installed.

## Validation and acceptance

- [x] `mise run check` passes.
- [x] `mise run build-windows` passes.
- [x] `mise run test-integration` passes.
- [x] A processor test proves `--keep-original` semantics by preserving the exact source bytes and publishing the HEVC-sized output at `source.hevc.mp4`.
- [x] A path-planning test proves changed-container input keeps `source.avi` separate from `source.mkv`.
- [x] Existing destination collision handling still skips without overwriting.
- [x] The safety policy in `README.md` still holds: output publication occurs only after structural validation, the configured full decode check, and the minimum-savings gate pass.

No separate real-directory `--dry-run` is required because discovery, codec
policy, and CRF selection do not change; the dry-run destination text is
covered by the same path planner.

## Idempotence and recovery

The edit and verification commands are repeatable. A keep-original run that
already produced `movie.hevc.mp4` or `movie.mkv` is skipped because the existing
collision guard refuses to overwrite it. Interrupted or rejected encodes leave
the source untouched, and the deferred cleanup removes the
`.<name>.ffmpeg-compressing-*` temporary output. If publication succeeds, both
files intentionally remain. Recovery consists of deleting the separately
named HEVC output after confirming its path; no source restoration is needed.
Default mode retains the existing atomic replacement and changed-container
removal behavior.

## Progress

- [x] (2026-08-19 14:52Z) Inspected configuration, CLI, path planning, processor publication, tests, and safety documentation.
- [x] (2026-08-19 14:52Z) Defined opt-in naming, collision, safety-gate, and disk-accounting semantics.
- [x] (2026-08-19 14:54Z) Added configuration, CLI, path planning, processor behavior, and explicit retained-source logging.
- [x] (2026-08-19 14:55Z) Added unit coverage for byte preservation, destination naming, permissions, collision refusal, and disk-space accounting.
- [x] (2026-08-19 14:55Z) Extended the real-FFmpeg integration test with replacement and retained-source subtests.
- [x] (2026-08-19 14:55Z) Updated `README.md` with the option, naming rules, and no-overwrite behavior.
- [x] (2026-08-19 14:56Z) Ran formatting, `mise run check`, host and Windows builds, CLI help inspection, and `mise run test-integration` successfully.
- [x] (2026-08-19 14:56Z) Recorded evidence and retrospective.
- [x] (2026-08-19 18:04Z) Diagnosed the Windows CI failure as a non-portable hard-coded permission expectation and changed the test to compare the output with the source filesystem's observed permissions.
- [ ] Re-run local checks, push the focused CI fix, and confirm the GitHub Actions rerun passes.

## Surprises and discoveries

The first sandboxed `go test ./...` attempted to download Go 1.26 and could not
write its module cache. The first sandboxed mise validation could invoke the
installed Go toolchain but could not read or write all files under the user's
external Go installation and cache; cross-compilation presented this as
missing standard-library packages such as `unsafe`. Trusting this repository's
`mise.toml` and rerunning the repository tasks with approved access resolved
the environment restriction. The same commands then passed without code
changes.

Windows GitHub Actions exposed a portability error in the new test rather than
in publication code. `os.WriteFile(path, data, 0640)` produced a file whose
observed permissions were `0666` on Windows because Windows does not implement
Unix permission bits in the same way. The failing assertion said
`output permissions = 666, want 640`. The behavioral contract is that the
output inherits the source's permissions as represented by the host
filesystem, so the test now captures `os.Stat(path).Mode().Perm()` and compares
the output against that value instead of a Unix-only literal.

## Decision log

2026-08-19 — Codex: Use `--keep-original` as an opt-in boolean whose default is
false. This preserves every existing invocation and makes the requested safety
behavior explicit.

2026-08-19 — Codex: Name same-container retained outputs
`<stem>.hevc<extension>`, while preserving the existing `<stem>.mkv` rule for
other containers. A same-container output cannot share the source path, and a
codec-bearing suffix is clearer than silently renaming the source to a backup.

2026-08-19 — Codex: Report zero aggregate saved bytes when the source is
retained. The compressed output may be smaller in isolation, but keeping both
files increases rather than reduces current disk use, so counting that size
difference as reclaimed storage would be misleading.

2026-08-19 — Codex: Verify permission inheritance against the source mode
observed by the running operating system. A hard-coded Unix mode tests Go's
platform-specific `os.WriteFile` behavior on Windows rather than vcompress's
promise to carry source attributes to the output.

## Artifacts and notes

`mise run check` completed with all packages passing and no `go vet`
diagnostics. The relevant lines were:

```text
[test] ok  vcompress/internal/fsutil
[test] ok  vcompress/internal/processor
[vet] Finished in 4.85s
Finished in 6.64s
```

`mise run build-windows` exited zero after
`go build -o vcompress.exe ./cmd/vcompress`.

`mise run test-integration` ran both publication modes against FFmpeg and
libx265. The fixture was 2.34 MiB and the validated output was 773.30 KiB, a
67.8% reduction. The retained-source line was:

```text
OK: crf=20 ... | 2.34 MiB -> 773.30 KiB | potential_saving=1.59 MiB (67.8%) | source retained=.../source.mp4 | .../source.hevc.mp4
```

Both `TestIntegrationMPEG4ToHEVC/replace_source` and
`TestIntegrationMPEG4ToHEVC/keep_original` passed. The keep-original subtest
probed the source as MPEG-4 and the separate output as HEVC. The built CLI's
`-help` output included `-keep-original` with the expected description.

The first PR run failed only `unit (windows-latest)` with:

```text
--- FAIL: TestProcessKeepOriginalPublishesBesideSource (0.00s)
    processor_test.go:141: output permissions = 666, want 640
```

Ubuntu unit tests, vet and integration tests, and security checks all passed.
Post-fix rerun evidence will be appended after the PR update.

## Outcomes and retrospective

The opt-in mode shipped in configuration, CLI parsing, path planning,
publication, logging, tests, and user documentation. Default behavior remains
unchanged. In keep-original mode all pre-publication validation and savings
gates remain active, the source is never replaced or removed, and aggregate
saved bytes remain zero because no storage was reclaimed. No planned scope was
left open. Windows CI required one test-only portability correction: permission
inheritance is now checked against the source mode reported by the host instead
of the Unix-specific literal `0640`; production behavior did not change.
