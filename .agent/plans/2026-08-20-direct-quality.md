# Direct CRF and NVENC CQ selection without SSIM analysis

## Purpose and big picture

`vcompress` currently sample-encodes representative intervals and compares
Structural Similarity Index Measure (SSIM) scores before every full encode.
That is a useful default safety check, but repeated sample encodes add
substantial elapsed time when processing roughly 3 TB of source video. This
change adds explicit `-crf N` and `-cq N` command-line modes. `-crf` uses the
software libx265 encoder and its Constant Rate Factor (CRF); `-cq` uses the
NVIDIA `hevc_nvenc` encoder and its Constant Quality (CQ) control. Either mode
selects the requested value immediately and performs no representative sample
encode or SSIM comparison.

Automatic SSIM-based selection remains the default when neither option is
present. Direct selection deliberately removes only the pre-encode SSIM gate.
Codec and HDR skip policy, stream and media-structure validation, the full
decode check, the minimum-savings check, temporary-output handling, and safe
publication or replacement remain unchanged. Supporting arbitrary encoder
options, changing codecs, running files concurrently, or disabling the
post-encode validation gates is out of scope.

## Context and orientation

`cmd/vcompress/main.go` parses CLI flags, probes FFmpeg's NVIDIA capabilities,
chooses the encoder and quality selector, and logs startup configuration.
`internal/config/config.go` owns defaults and validation. `internal/quality/selector.go`
currently tries quality values 20, 18, then 16 and measures samples through
`internal/ffmpeg/client.go`. `internal/processor/processor.go` probes each file,
applies codec and HDR policy, asks a selector for a quality result, performs the
full encode, validates it, checks savings, and only then publishes it.
`internal/processor/policy.go`, `internal/media/media.go`, and
`internal/fsutil/replace_unix.go` or `internal/fsutil/replace_windows.go`
provide the safety gates that this change must not weaken.

CRF is libx265's integer quality control from 0 through 51; lower values retain
more detail and usually produce larger files. CQ is the corresponding integer
constant-quality control accepted by FFmpeg's `hevc_nvenc`, also from 0 through
51, but the numeric values of CRF and CQ are not quality-equivalent. SSIM is an
objective reconstructed-image comparison in which values nearer 1 indicate a
closer match. A full decode check makes FFmpeg decode the entire generated
video with errors treated as fatal. An atomic rename publishes a fully written
file as one filesystem operation, so readers do not observe a partial output.
Generational loss is quality loss caused by decoding and re-encoding an already
compressed source.

The present automatic path selects NVIDIA when its real runtime probe succeeds
and otherwise selects libx265. If NVENC sample analysis fails, it retries the
whole selection with libx265. Direct options instead name an encoder-specific
control: `-crf` therefore forces libx265 even when NVENC exists, while `-cq`
requires the NVIDIA runtime probe to succeed and does not silently reinterpret
the value as libx265 CRF.

## Interfaces and dependencies

`internal/config.Config` gains optional `DirectCRF *int` and `DirectCQ *int`
fields. `nil` means not supplied, which preserves CRF 0 and CQ 0 as valid,
explicit values. `Config.Validate` rejects values outside 0 through 51 and
rejects simultaneous `-crf` and `-cq` use. Defaults leave both fields nil.

`cmd/vcompress/main.go` registers `-crf` and `-cq` using parsing callbacks so
the absence of each option is distinguishable from an explicit zero. A small
selector-construction helper returns the selected `processor.Selector`, the
effective encoder name, and a human-readable quality-mode string. Direct CQ
construction returns an error if `client.NVIDIA.NVENC` is false.

`internal/quality.Result` renames the encoder-neutral numeric field from `CRF`
to `Value` and adds `SSIMCompared bool`. The existing `quality.Selector` sets
`SSIMCompared` for successful automatic selection. A new
`quality.FixedSelector` has `Value int` and `Encoder string` fields and exposes
the existing selector contract:

```go
Select(context.Context, string, int, string, float64) (quality.Result, error)
```

It returns a found result immediately without calling an SSIM measurer.
`ffmpeg.EncodeOptions.CRF` likewise becomes the encoder-neutral `Quality int`;
`Client.Encode` maps it to `-crf:v:N` for libx265 or `-cq:v:N` for
`hevc_nvenc`. No new external dependency or FFmpeg version requirement is
introduced.

## Plan of work

First add optional direct values and their validation in `internal/config` so
invalid or ambiguous CLI states cannot reach media processing. Then make the
quality result and final encode option encoder-neutral and add the fixed
selector, with unit tests proving that automatic results are marked as SSIM
compared and fixed results return directly.

Next thread the selector mode through `cmd/vcompress/main.go`. Automatic mode
must construct the same preferred and fallback encoders as today. CRF mode
must construct a fixed libx265 result, CQ mode a fixed NVENC result after a
successful runtime probe. Processor logging will clearly distinguish measured
selection from a direct override and must never print zero-valued SSIM scores
as if a comparison happened.

Finally update the existing README usage and safety description, add processor
and real-FFmpeg coverage for the direct path, format all Go sources, and run
the full unit, vet, host build, Windows compile, integration, and generated
fixture dry-run checks. The direct path will be accepted only if the generated
output still passes every post-encode validation and savings gate before
publication.

## Concrete steps

Run all commands from `/Users/jun_networks/workspace/vcompress`.

```bash
mise run fmt
mise run check
mise run build
mise run build-windows
mise run test-integration
```

The check must report all package tests passing and `go vet` must be silent.
Both builds must exit zero. The integration test must produce HEVC output from
a generated MPEG-4 fixture using libx265, including the direct selector path.

For an observable dry run, generate a short disposable MPEG-4 file outside the
repository and run the built binary with `-crf 24 -dry-run`. The log must show
direct CRF selection and must contain no `QUALITY-TEST` line:

```bash
fixture_dir=$(mktemp -d)
ffmpeg -hide_banner -loglevel error -y -f lavfi -i testsrc2=size=320x180:rate=24 -t 2 -c:v mpeg4 "$fixture_dir/source.mp4"
./vcompress -crf 24 -dry-run "$fixture_dir"
```

Expected relevant output resembles `QUALITY-DIRECT: encoder=libx265 crf=24
ssim=skipped` followed by a dry-run line; the source remains unchanged.

## Validation and acceptance

- [x] `mise run check` passes all unit tests and vet checks.
- [x] `mise run build` produces the host binary.
- [x] `mise run build-windows` confirms the Windows build still compiles.
- [x] `mise run test-integration` completes a real FFmpeg/libx265 round trip,
      including direct CRF selection.
- [x] A generated-fixture `-crf 24 -dry-run` logs direct selection with no
      `QUALITY-TEST` or SSIM comparison.
- [x] Config tests reject conflicting flags and values outside 0 through 51.
- [x] Selector tests show automatic mode is marked as measured and fixed mode
      is not; processor tests show direct mode encodes and logs no fake SSIM.
- [x] The safety policy in `README.md` still holds: sources are replaced only
      after structural validation, the enabled full decode check, and the
      minimum-savings gate pass. Direct mode knowingly bypasses only the SSIM
      quality comparison and communicates that fact in startup and per-file
      logs.

## Idempotence and recovery

Formatting, tests, builds, and dry runs are safe to repeat. Host and Windows
builds overwrite only repository build artifacts `vcompress` and
`vcompress.exe`; these are ignored build products and can be rebuilt. The
fixture is created under a unique temporary directory and never targets user
media.

Normal processing still writes `.<name>.ffmpeg-compressing-*` beside each
source. `Processor.Process` defers removal of this path, so failed or
interrupted work leaves the source untouched and normally removes the partial
output. Re-running rediscovers the original source. Existing final destinations
are skipped rather than overwritten. To recover the code change, revert only
the files listed in this plan with a normal version-control revert; never use a
broad destructive reset in a worktree containing user changes.

## Progress

- [x] (2026-08-20 02:40Z) Inspected configuration, quality selection, encoder,
      processing, safety policy, tests, and repository planning requirements.
- [x] (2026-08-20 02:40Z) Chose explicit encoder-specific `-crf` and `-cq`
      modes while retaining every post-encode safety gate.
- [x] (2026-08-20 02:46Z) Added configuration fields, CLI parsing, selection
      strategy, and tests.
- [x] (2026-08-20 02:46Z) Added direct selector/result semantics and processor
      logging coverage.
- [x] (2026-08-20 02:46Z) Updated the existing README to describe the opt-in
      bypass and retained validation guarantees.
- [x] (2026-08-20 02:46Z) Ran formatting, unit/vet, host build, Windows build,
      integration, and generated-fixture dry-run validation.
- [x] (2026-08-20 02:46Z) Recorded evidence and completed the retrospective.

## Surprises and discoveries

The existing encoder-neutral quality value is named `CRF` even though the
NVENC path already passes it to `-cq`. Renaming the internal fields to `Value`
and `Quality` avoids making the new public CQ mode look like an accidental CRF
alias.

The first `mise run fmt` attempt was blocked because the managed filesystem did
not allow access to Go's cache under `~/Library/Caches/go-build`. Re-running the
repository task with the approved external cache access succeeded; this was an
environment restriction rather than a source or formatting failure.

## Decision log

2026-08-20 — Codex: Add separate `-crf` and `-cq` options instead of one
encoder-neutral quality flag. Their numeric scales are not equivalent, and an
encoder-specific option makes the performance and quality choice explicit.

2026-08-20 — Codex: Make `-crf` force libx265 and `-cq` require a successful
NVENC runtime probe. Silently falling back would reinterpret the same number
under different rate-control semantics and violate the user's explicit choice.

2026-08-20 — Codex: Retain structural validation, full decoding, minimum
savings, and safe publication in direct mode. The requested performance gain
comes from skipping sample analysis only; weakening output-integrity or source
replacement gates would create unrelated data-loss risk.

2026-08-20 — Codex: Represent unspecified direct values with nil pointers.
Using zero as the unset sentinel would make valid CRF 0 and CQ 0 impossible to
request, while using -1 would expose a confusing default in CLI help.

## Artifacts and notes

Initial worktree state was clean on `codex/nvidia-zero-copy` at commit
`b645612`.

`mise run check` completed with all packages passing and `go vet` silent.
`mise run build` and `mise run build-windows` both exited zero. The integration
suite added `TestIntegrationMPEG4ToHEVC/direct_CRF_without_SSIM`; its relevant
output was:

```text
QUALITY-DIRECT: encoder=libx265 crf=24 ssim=skipped
OK: encoder=libx265 crf=24 ssim=skipped | 2.34 MiB -> 534.00 KiB | saved=1.82 MiB (77.8%)
--- PASS: TestIntegrationMPEG4ToHEVC/direct_CRF_without_SSIM (1.02s)
```

The generated-fixture dry run exited zero and the filtered log contained no
`QUALITY-TEST` or `QUALITY-SELECT` line:

```text
START root=/private/tmp/vcompress-direct-quality.Nq0aMO quality_mode=direct-crf:24:ssim-skipped encoder=libx265 decoder=software ... dry_run=true
QUALITY-DIRECT: encoder=libx265 crf=24 ssim=skipped
DRY-RUN: selected encoder=libx265 crf=24 ssim=skipped; would encode to /private/tmp/vcompress-direct-quality.Nq0aMO/source.mp4
DONE total=1 converted=0 skipped=1 failed=0 saved=0 B
```

The fixture SHA-256 was
`257c1e221df40beeca08601f8b16fe3a7f61fc8c7204770e8757fe51408f99e7`
both before and after the dry run. CLI checks returned exit status 2 and the
expected messages `crf and cq cannot be used together` for conflicting direct
options and `crf must be between 0 and 51` for `-crf 52`. On the validation
host, `-cq 24 -dry-run` returned exit status 1 before traversal with
`-cq requires working NVIDIA NVENC: hevc_nvenc is not present in this FFmpeg
build`, confirming the real startup guard as well as its unit coverage.

## Outcomes and retrospective

The implementation adds explicit direct libx265 CRF and NVENC CQ modes while
leaving SSIM-based 20/18/16 selection as the default. Direct CQ refuses to
start without a successful NVENC runtime probe, rather than silently changing
encoder or rate-control semantics. Per-file and startup logs explicitly state
that SSIM was skipped.

All planned automated and empirical validation passed. The direct libx265
integration path proved that the fixed result reaches the real encoder and
still passes structural checks, a full decode, the savings gate, and safe
publication. NVENC CQ selection and its unavailable-device failure are covered
with unit tests because the validation host has no usable NVIDIA runtime. No
planned work remains.
