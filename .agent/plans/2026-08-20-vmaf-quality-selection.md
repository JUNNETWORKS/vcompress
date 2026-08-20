# VMAF quality selection

## Purpose and big picture

`vcompress` will use Video Multi-Method Assessment Fusion (VMAF), a perceptual full-reference video quality score, for automatic quality selection by default instead of relying only on Structural Similarity Index Measure (SSIM). Users can choose `vmaf`, `ssim`, or `both`; `both` accepts a candidate only when both metrics pass. Direct `-crf` and `-cq` modes will continue to bypass sample analysis without weakening finished-file structural validation, full decoding, minimum-savings enforcement, or safe publication.

This change does not add HDR support, change the 20, 18, and 16 candidate values, change codec eligibility, or change source replacement. VMAF is only used on Standard Dynamic Range (SDR) inputs already accepted by policy.

## Context and orientation

`internal/config/config.go` owns defaults and validation. `cmd/vcompress/main.go` owns command-line flags, startup capability checks, and construction of the quality selector. `internal/quality/selector.go` spreads short samples through a video and chooses the highest candidate quality value whose average and worst-sample scores pass. `internal/ffmpeg/client.go` performs sample encoding and objective comparison. `internal/processor/processor.go` logs the selected quality and gates the complete encode and safe replacement. Their matching `_test.go` files and `internal/processor/integration_test.go` provide unit and real-FFmpeg coverage. `README.md` describes the user contract.

Before this change, libx265 emits its own reconstructed-frame SSIM while NVENC writes a temporary HEVC sample and compares it to the source through FFmpeg's `ssim` filter. SSIM is a full-reference image similarity measure ranging from zero to one, where one means identical. VMAF combines multiple image features using a trained model and normally reports scores from zero to 100, where higher is better. A representative sample is a short interval selected from a longer video. CRF is libx265's Constant Rate Factor, and CQ is NVENC's Constant Quality value; lower values generally retain more detail. A full-reference metric compares decoded compressed frames with matching source frames.

VMAF requires FFmpeg's `libvmaf` video filter, which is only present when FFmpeg was built with libvmaf. Automatic `vmaf` and `both` modes must fail before traversal if that filter is unavailable. `ssim` mode and direct CRF/CQ mode do not require it.

## Interfaces and dependencies

`config.Config` will gain a `QualityMetric string`, `VMAFAverageMin float64`, and `VMAFWorstMin float64`. `QualityMetric` defaults to `vmaf` and accepts only `vmaf`, `ssim`, or `both`, case-insensitively after normalization. VMAF thresholds default to 95 for the mean across representative samples and 90 for the lowest representative-sample score, and must satisfy `0 <= worst <= average <= 100`. Existing SSIM defaults remain 0.995 and 0.992 and are validated even when not selected so configuration is deterministic.

The CLI will add `-quality-metric`, `-vmaf-average`, and `-vmaf-worst`. Existing `-ssim-average` and `-ssim-worst` remain available. `-crf` and `-cq` bypass every selected comparison metric.

`internal/quality` will define a metric mode and a score/result structure that can carry both VMAF and SSIM aggregates. The measurer contract will request the selected metric or metrics for one encoded sample and return the corresponding values. A candidate passes only when every metric enabled by the mode passes its average and worst thresholds.

`internal/ffmpeg.Client` will expose a read-only `HasLibvmaf(context.Context) error` capability probe and a sample-measurement method. Each sample will be encoded once to a temporary Matroska file with libx265 or NVENC, then decoded alongside the same source interval. FFmpeg's `libvmaf` filter receives the distorted stream as its main input and the source stream as its reference input, with timestamps reset to zero and `shortest=1`. The default bundled `vmaf_v0.6.1` model is used. SSIM uses the same encoded sample through the `ssim` filter. Both comparisons run on host frames because these filters are software filters; NVDEC may decode into host memory but CUDA frames are not retained for comparison. VMAF is parsed from FFmpeg's final `VMAF score:` line and SSIM from the final `SSIM Y:` line.

No new Go module is required. Runtime FFmpeg must provide libx265 as before and must additionally provide `libvmaf` when automatic mode includes VMAF.

## Plan of work

First, add configuration vocabulary and validation so downstream packages receive a closed set of modes and thresholds. Then generalize the quality selector from one scalar SSIM score to named metric scores while preserving candidate ordering, all-sample aggregation, encoder fallback, and fail-closed behavior. Next, consolidate FFmpeg sample encoding so libx265 and NVENC produce comparable temporary samples, add VMAF parsing and metric comparison, and retain NVDEC fallback behavior. Wire the CLI flags, startup capability check, selector construction, and metric-aware logs after those contracts compile.

Update processor summaries and failure wording so they never claim SSIM was used in VMAF or direct mode. Update unit and integration tests before documenting the new default. Finally, exercise the actual locally installed libvmaf filter, run all CI-equivalent checks and the Windows cross-build, and record empirical output. The replacement gate remains untouched; only a candidate that passes the configured analysis proceeds to the existing complete encode and validation path.

## Concrete steps

Run all commands from `/Users/jun_networks/workspace/vcompress`.

```bash
mise run check
mise run build-windows
mise run test-integration
```

For the real metric path, generate a short SDR MPEG-4 fixture in a temporary directory, run `vcompress -dry-run -quality-metric vmaf` against it, and inspect `ffmpeg-compress.log`. Expect `QUALITY-TEST` and `QUALITY-SELECT` records containing VMAF fields and no source replacement. Repeat with `-quality-metric both` and expect both VMAF and SSIM fields. A direct `-crf` dry run must report that analysis was skipped and must not require libvmaf.

## Validation and acceptance

- [x] `mise run check` passes all Go tests and `go vet`.
- [x] `mise run build-windows` compiles the Windows implementation.
- [x] `mise run test-integration` performs the real FFmpeg/libx265 replacement and keep-original paths, including automatic VMAF when libvmaf is available.
- [x] A `--dry-run` pass logs VMAF-only scores by default; `both` logs and enforces both metrics; `ssim` remains selectable.
- [x] Invalid metric names and invalid VMAF thresholds fail configuration validation.
- [x] VMAF mode fails clearly at startup when FFmpeg has no `libvmaf` filter, while SSIM and direct modes remain usable.
- [x] The safety policy in `README.md` still holds: sources are only published or replaced after structural validation, the optional-by-explicit-flag full decode check, and the minimum savings gate pass.

Expected behavior is three quality candidates at most, one encoded temporary sample per representative interval and candidate, and acceptance of the first candidate for which all selected metrics satisfy both aggregate thresholds.

## Idempotence and recovery

Unit tests, builds, and dry runs are repeatable. Metric samples use unique operating-system temporary files and deferred cleanup, including error returns. Per-file complete encodes continue to use `.<name>.ffmpeg-compressing-*` beside the source and `internal/processor/processor.go` removes an unpublished temporary output on return. An interrupted run may be repeated; already-efficient completed outputs are skipped by policy, and existing side-by-side destinations are never overwritten.

If implementation is interrupted, inspect `git diff` and resume from Progress. Revert only files listed in this plan if abandoning the change; do not remove unrelated work. No migration or persistent state is introduced.

## Progress

- [x] (2026-08-20 04:18Z) Inspected the SSIM selector, FFmpeg paths, CLI wiring, processor gate, tests, and local libvmaf availability.
- [x] (2026-08-20 04:20Z) Added metric configuration, defaults, flags, and validation.
- [x] (2026-08-20 04:20Z) Generalized selection and result logging for VMAF, SSIM, and both.
- [x] (2026-08-20 04:20Z) Implemented shared temporary sample encoding and FFmpeg VMAF/SSIM comparison.
- [x] (2026-08-20 04:21Z) Added and updated unit and integration tests.
- [x] (2026-08-20 04:21Z) Updated `README.md` with runtime requirements and usage.
- [x] (2026-08-20 04:23Z) Ran formatters, CI checks, Windows build, integration tests, and real dry runs.
- [x] (2026-08-20 04:24Z) Recorded evidence and retrospective.

## Surprises and discoveries

The locally installed FFmpeg 7.1.1 reports both `libvmaf` and `ssim` filters and was configured with `--enable-libvmaf`. This permits an empirical VMAF integration run on the development host.

The existing libx265 path reads encoder-internal reconstructed-frame SSIM without creating a sample file, whereas NVENC already writes a sample. Supporting VMAF consistently requires consolidating these paths around an encoded sample file.

The sandbox does not permit the default macOS Go build cache under `~/Library/Caches/go-build`. Verification succeeded with `GOCACHE=/tmp/vcompress-go-cache`, without changing repository configuration.

## Decision log

2026-08-20 — Codex: Make `vmaf` the automatic default and retain explicit `ssim` and `both` modes. This directly fulfills the requested move to VMAF while preserving the existing metric for compatibility and providing a stricter conjunction when desired.

2026-08-20 — Codex: Default VMAF thresholds to average 95 and worst representative sample 90. These are conservative perceptual-quality starting points on VMAF's zero-to-100 scale; they remain explicit CLI policy knobs because VMAF scores are content- and model-dependent.

2026-08-20 — Codex: Fail closed when the configured VMAF filter is unavailable instead of silently reverting to SSIM. Silent fallback would make the logged and intended safety policy differ.

2026-08-20 — Codex: In `both` mode, require both metric gates. Treating either metric as sufficient would weaken rather than combine the policies.

2026-08-20 — Codex: Pin the comparison filter to `vmaf_v0.6.1` instead of relying on libvmaf's implicit default. This keeps scores and configured thresholds stable if a future FFmpeg build changes its default model.

## Artifacts and notes

Local capability observation:

```text
ffmpeg version 7.1.1
configuration: ... --enable-libvmaf ... --enable-libx265 ...
... libvmaf           VV->V      Calculate the VMAF between two video streams.
TS. ssim              VV->V      Calculate the SSIM between two video streams.
```

The final CI-equivalent checks completed successfully:

```text
mise run check
ok   vcompress/cmd/vcompress
ok   vcompress/internal/config
ok   vcompress/internal/ffmpeg
ok   vcompress/internal/processor
ok   vcompress/internal/quality
go vet: passed

mise run build-windows
go build -o vcompress.exe ./cmd/vcompress

mise run test-integration
--- PASS: TestIntegrationMPEG4ToHEVC (4.64s)
QUALITY-TEST: encoder=libx265 value=20 ... vmaf=96.9387
QUALITY-SELECT: encoder=libx265 value=20 avg_vmaf=96.9387 worst_vmaf=96.9387
```

Real dry-run evidence from the same 640x360 MPEG-4 fixture:

```text
quality_mode=vmaf-auto:20/18/16
QUALITY-TEST: ... vmaf=96.4326

quality_mode=both-auto:20/18/16
QUALITY-TEST: ... vmaf=96.4326 ssim=0.9921560

quality_mode=ssim-auto:20/18/16
QUALITY-TEST: ... ssim=0.9921560

quality_mode=direct-crf:24:analysis-skipped
QUALITY-DIRECT: encoder=libx265 crf=24 analysis=skipped
```

## Outcomes and retrospective

Automatic selection now defaults to VMAF with explicit 95 average and 90 worst-sample thresholds. Users can select the legacy SSIM policy or require both policies, and all modes log only the metrics they actually used. libx265 and NVENC analysis now share the same sample-file comparison design, which avoids comparing different reconstructed artifacts across encoders. Direct CRF/CQ retains its explicit analysis bypass and does not require libvmaf.

Unit tests, `go vet`, the Windows cross-build, the real FFmpeg/libx265 integration suite, and VMAF/SSIM/both/direct dry runs passed. The integration path replaced or published outputs only after the unchanged structural, full-decode, and minimum-savings gates. No work remains in scope.
