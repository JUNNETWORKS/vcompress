# Select multiple directories and files in the WebUI

## Purpose and big picture

The WebUI currently accepts one directory. After this change, a person can type
a path into the existing server-side picker and move directly to it, add more
than one directory, and add individual supported video files. All selected
targets form one sequential job with one aggregate progress view. Selecting a
directory remains recursive, while selecting a file processes only that file.

The command-line interface remains intentionally unchanged: it still accepts
exactly one directory. The browser does not upload media and does not receive
raw file-system handles; it sends absolute paths to the localhost Go server as
it does today. The validation gates, codec skip policy, temporary-file layout,
full decode check, minimum-savings check, and publication path are out of scope
and must remain unchanged.

## Context and orientation

`internal/webui/static/index.html`, `internal/webui/static/style.css`, and
`internal/webui/static/app.js` implement the embedded browser interface. The
current picker calls `GET /api/directories`, displays only directories, and
writes one path into `config.Config.Root`.

`internal/webui/server.go` serves the API and lists directory entries.
`internal/webui/manager.go` owns one running job and snapshots its progress.
`internal/config/config.go` carries the processing settings from both the CLI
and WebUI. `internal/job/job.go` validates the runtime, discovers candidates,
creates one processor, and processes candidates sequentially.
`internal/discovery/discovery.go` recognizes supported video extensions and
recursively lists candidates. `internal/processor/processor.go` performs the
per-file safety-critical conversion and publication workflow; this plan does
not change it. `README.md` describes WebUI target selection and the log
location rule.

A target is an absolute path selected by the user. A target can be a directory
or a supported video file. A candidate is a unique video file produced after
expanding directory targets recursively. If a file is selected directly and
also appears inside a selected directory, it is one candidate, not two.

## Interfaces and dependencies

`config.Config` gains `Targets []string` with JSON name `targets` and
`omitempty`. `Config.Normalize` accepts either the existing non-empty `Root`
or at least one target. It converts every supplied target to a clean absolute
path, removes exact duplicates while preserving selection order, and leaves
the existing `Root` behaviour unchanged. When `Targets` is non-empty it is the
effective input set; otherwise the effective set is the one-element list
containing `Root`.

`config.Config` gains `TargetPaths() []string`, returning a defensive copy of
the effective input set.

`discovery.ListTargets(targets []string) ([]string, error)` validates every
target with `os.Stat`. A directory is traversed with the existing `Walk`; a
regular file must satisfy `IsVideoPath`; other file types and unsupported file
extensions return an error. The result contains clean absolute candidate paths
in target and traversal order, with duplicates removed. `List(root string)`
becomes a compatibility wrapper around `ListTargets([]string{root})`.

`job.Run` keeps its signature. It reads `cfg.TargetPaths()`, chooses the log
directory from the first target (the target itself for a directory, or its
parent for a file), and discovers candidates through `ListTargets`. One
`ffmpeg-compress.log` is therefore written beside the first selected file or
inside the first selected directory. The processing loop and processor calls
remain unchanged.

`webui.Snapshot` gains `Targets []string`. `Manager.Start` copies the normalized
targets into the snapshot. The existing `RunFunc` signature stays unchanged
because targets travel in `config.Config`.

The directory listing API keeps `GET /api/directories?path=...` for
compatibility. Each entry gains `kind`, whose value is `directory` or `file`.
The API returns directories plus supported video files and omits other regular
files. Directories sort before files, then case-insensitively by name.

The browser keeps a selected-target collection keyed by absolute path. The
picker path becomes an editable text input with a Move action and Enter-key
navigation. Each listed directory has separate Open and Add actions; each
listed video file has an Add action. The current directory can also be added.
The main form displays every selected target with its kind and a Remove action.
A job cannot start with an empty target collection.

No new Go module, JavaScript package, native dialog library, or external
runtime dependency is introduced.

## Plan of work

First extend configuration and discovery, because the WebUI must not advertise
multiple targets before the execution layer can safely validate and deduplicate
them. Unit tests will establish mixed directory/file expansion, overlap
deduplication, unsupported-file rejection, and unchanged single-root behavior.

Next adapt `internal/job/job.go` and `internal/webui/manager.go` without touching
the per-file processor. This keeps the safety boundary explicit: only the list
of inputs changes, while every candidate still passes through the same probe,
quality selection, encode, validation, savings, and publication sequence.

Then expand the directory API and embedded UI. The selected collection will be
rendered from JavaScript state rather than encoded as one text field. The
picker will distinguish navigation from selection so opening a directory does
not implicitly add it. Direct path navigation will use the same API validation
as button navigation.

Finally run automated tests, cross-compile for Windows, execute a real FFmpeg
dry run over overlapping directory and file targets, and operate the embedded
page in a real browser. The browser check will confirm direct path movement,
multiple directory additions, file addition, removal, request payload, and
responsive layout.

## Concrete steps

Run all commands from `/Users/jun_networks/workspace/vcompress`.

```bash
gofmt -w internal/config/config.go internal/config/config_test.go \
  internal/discovery/discovery.go internal/discovery/discovery_test.go \
  internal/job/job.go internal/job/job_test.go \
  internal/webui/manager.go internal/webui/manager_test.go \
  internal/webui/server.go internal/webui/server_test.go
node --check internal/webui/static/app.js
mise run check
mise run build
mise run build-windows
go test -race ./internal/webui
mise run test-integration
git diff --check
```

For empirical WebUI validation, build `vcompress`, create two temporary
directories with a small FFmpeg-generated MPEG-4 fixture, start
`./vcompress web --no-open --port 18080`, select overlapping directory and file
targets in the browser, and submit a direct-CRF dry run. Expect one result row
per unique file and no source modification.

## Validation and acceptance

- [x] `mise run check` passes, including new configuration, discovery, API, and manager tests.
- [x] `mise run build` produces the host binary.
- [x] `mise run build-windows` proves the full program still compiles for Windows.
- [x] `mise run test-integration` passes the existing real libx265 round trip.
- [x] `go test -race ./internal/webui` reports no race.
- [x] `node --check internal/webui/static/app.js` reports no syntax error.
- [x] A direct path typed into the picker moves on Enter; the Move button is bound to the same tested `moveToTypedPath` handler.
- [x] Two directories and one individual video can be added and independently removed.
- [x] An overlapping directory and direct-file selection produces one candidate for the duplicate file.
- [x] Unsupported regular files are not offered by the picker and are rejected by discovery if sent directly to the API.
- [x] A real direct-CRF dry run leaves every source byte-for-byte unchanged.
- [x] The safety policy in `README.md` still holds: a non-dry-run source can only be replaced after structural validation, full decoding when enabled, and the minimum-savings gate.

## Idempotence and recovery

Configuration normalization and discovery are repeatable. Exact duplicate
targets and overlapping discoveries are removed on every run, so retrying does
not multiply work. A dry run writes only `ffmpeg-compress.log` in the first
target's directory and does not create a published output.

Normal encodes continue to create a hidden
`.<name>.ffmpeg-compressing-*` temporary output beside each source. Cancellation
causes the processor's deferred cleanup to remove an unpublished temporary
output. Re-running after cancellation rediscovers the original targets. If a
validated keep-original output already exists, the existing refusal to
overwrite it remains active.

If implementation fails partway, the branch can remain with uncommitted files;
no source fixture should be reused until its checksum is compared with the
recorded pre-run value. Temporary test directories are disposable and can be
removed after the server and FFmpeg child have stopped.

## Progress

- [x] (2026-08-20 10:34Z) Inspected the current WebUI, configuration, discovery, job, and processor boundaries and wrote this ExecPlan.
- [x] (2026-08-20 10:40Z) Added normalized multi-target configuration and discovery contracts with tests.
- [x] (2026-08-20 10:40Z) Ran mixed targets through the existing sequential job and exposed them in manager snapshots.
- [x] (2026-08-20 10:40Z) Expanded the directory API to return directories and supported video files.
- [x] (2026-08-20 10:40Z) Implemented editable navigation and the selected-target collection in the embedded UI.
- [x] (2026-08-20 10:44Z) Completed automated, cross-platform, real-media, and browser validation.
- [x] (2026-08-20 10:44Z) Recorded evidence and completed the retrospective.

## Surprises and discoveries

The current `config.Config.Root` is used both as the discovery root and as the
location of `ffmpeg-compress.log`, even though the processor itself does not
use it. Multi-target support therefore needs an explicit log-location rule but
does not require changing the safety-critical processor.

The real browser request intentionally contained an overlapping direct file:
the selected targets were `alpha`, `beta`, and `alpha/nested/a.mp4`, while the
completed summary reported `total=2`. Candidate-level deduplication therefore
worked after recursive expansion rather than relying on browser state.

## Decision log

2026-08-20 — Codex: Carry WebUI targets in `config.Config` and keep
`job.Run`'s signature. This preserves the manager's injectable run function and
keeps the CLI on its existing `Root` field. A separate WebUI-only job request
type was rejected because it would duplicate every processing option and add a
translation layer with no safety benefit.

2026-08-20 — Codex: Deduplicate candidates after recursive expansion, not only
the selected targets. This prevents processing the same source twice when a
directory and one of its files, or nested directories, are both selected.

2026-08-20 — Codex: Write one log in the first target's containing directory.
Writing into every selected directory would require fan-out logging and could
partially fail after processing began; a global user-data location would be a
new platform-specific policy. The first-target rule is deterministic and
matches the existing selected-root behavior.

2026-08-20 — Codex: Keep all file bytes on the server side. Browser-native file
handles and upload controls were rejected because they do not expose an
absolute path to FFmpeg and would require copying large media through the
browser, rebuilding the publication safety path.

## Artifacts and notes

`mise run check` completed with all package tests and `go vet ./...` passing.
`mise run build`, `mise run build-windows`, and
`go test -race ./internal/webui` completed without output errors. The real
integration test reported all three cases passing:

```text
--- PASS: TestIntegrationMPEG4ToHEVC
    --- PASS: TestIntegrationMPEG4ToHEVC/replace_source
    --- PASS: TestIntegrationMPEG4ToHEVC/keep_original
    --- PASS: TestIntegrationMPEG4ToHEVC/direct_CRF_without_analysis
```

The browser dry run submitted two directories and one overlapping direct file.
The final API snapshot contained:

```text
state=completed total=2 processed=2 converted=0 skipped=2 failed=0
targets=[alpha beta alpha/nested/a.mp4]
results=[alpha/nested/a.mp4 beta/b.mp4]
```

SHA-256 before and after the dry run was identical:

```text
91ada61b4aebad0b4860bee96a374781deccd9941d47a2718c716f07ad3a63b5  a.mp4
65a040907ae2bc433ab9e21355dc62e8be419719f1d5cef98186cf8c0f7bb48e  b.mp4
```

The search for hidden `.*.ffmpeg-compressing-*` outputs returned no paths.
Visual inspection showed the three selected targets with distinct `DIR` and
`FILE` badges, two result rows, and no overflow at the desktop test viewport.

## Outcomes and retrospective

The WebUI now supports direct typed navigation, multiple directory selection,
individual video selection, removal, reload-safe target snapshots, and
candidate deduplication across overlapping inputs. The CLI continues to accept
one directory and uses the same compatibility path through `job.Run`.

The implementation stayed within the planned boundary: no processor policy,
validation gate, temporary output, or publication code changed. One future
enhancement would be persisting target kinds in the job snapshot so a reloaded
completed page can restore `DIR` and `FILE` badges instead of the generic
`TARGET` badge; this does not affect execution or safety.
