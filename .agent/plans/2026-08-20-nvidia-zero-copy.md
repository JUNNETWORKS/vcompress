# NVIDIA zero-copy transcoding and frame-timing preservation

## Purpose and big picture

When both NVDEC and NVENC are usable, `vcompress` will keep decoded video
frames in NVIDIA GPU memory from decode through HEVC encode. The current
implementation invokes NVDEC but lets FFmpeg copy every decoded frame back to
host memory before NVENC uploads it again. Removing that round trip reduces
PCIe traffic and CPU overhead and makes the final file encode and NVENC sample
encodes fully hardware-backed. Operations that inherently require a software
filter, such as Structural Similarity Index Measure (SSIM) comparison, and the
libx265 encoder will continue to receive host-memory frames.

The final encode will also request passthrough frame timing so variable frame
rate input is not duplicated or dropped by FFmpeg's output synchronization.
The existing codec policy, quality thresholds, validation, full decode check,
minimum savings gate and source replacement behavior are unchanged. GPU
scaling, multiple simultaneous encodes, multi-GPU scheduling and hardware-
specific experimental NVENC options are out of scope.

## Context and orientation

`internal/ffmpeg/client.go` builds every FFmpeg command. Its current
`appendHWAccel` helper adds only `-hwaccel cuda`. NVIDIA Video Codec SDK
guidance distinguishes this from a zero-copy pipeline: without
`-hwaccel_output_format cuda`, decoded frames are copied to host memory; with
that option, frames remain CUDA hardware frames and can be handed directly to
`hevc_nvenc`. CUDA is NVIDIA's GPU programming and memory interface. NVDEC is
the dedicated NVIDIA video decoder, and NVENC is the dedicated NVIDIA video
encoder. Zero-copy means that the decoded frame does not cross from GPU memory
to CPU-accessible host memory between those two hardware blocks.

`internal/ffmpeg/client.go` has four relevant operation classes. A libx265
sample encode requires host frames because libx265 is a CPU encoder. An NVENC
sample encode can remain on the GPU. The subsequent SSIM comparison uses
FFmpeg's software `ssim` filter and therefore requires host frames. The final
NVENC encode can remain on the GPU. A full decode check writes frames to the
null muxer and can retain CUDA frames because it only verifies that the entire
video decodes without error.

`internal/ffmpeg/client_test.go` uses a fake process runner to inspect exact
arguments and simulate a failed hardware attempt followed by software decode.
`cmd/vcompress/main.go` reports the selected encoder and decoder path at
startup. `README.md` describes the acceleration and safety behavior. The
per-file orchestrator in `internal/processor/processor.go` remains unchanged
and still publishes only a fully validated, sufficiently smaller temporary
output.

Pixel format is the decoded chroma layout and bit depth, such as `yuv420p` or
`yuv420p10le`. FFmpeg represents a CUDA hardware frame with the outer pixel
format `cuda` while retaining its actual software layout as hardware-frame
metadata. Forcing `-pix_fmt yuv420p` on a zero-copy input can make FFmpeg insert
an unsupported CUDA-to-software conversion. The zero-copy attempt must
therefore omit the explicit output pixel-format option and allow NVENC to use
the decoded hardware frame's underlying layout. The existing structural
validation still rejects the result if its probed pixel format differs from
the source. A software-decode retry must restore the explicit `-pix_fmt` option.

## Interfaces and dependencies

No exported Go interface changes. In `internal/ffmpeg/client.go`, replace the
boolean-only hardware argument helper with a helper that can distinguish an
NVDEC host-output operation from an NVDEC CUDA-output operation. It must append
`-hwaccel cuda` for either hardware mode and additionally append
`-hwaccel_output_format cuda` only for zero-copy mode, always before the
associated `-i` input.

Extend `(*Client).runWithNVDECFallback` so each caller states whether its
hardware attempt should retain CUDA frames. The retry contract remains exact:
after any non-cancellation hardware failure, repeat the operation without
either hardware option. NVENC sample and final encode arguments include
`-pix_fmt` only on a host-frame path. libx265 and SSIM operations continue to
specify or consume host pixel formats as before.

The NVDEC startup probe and full decode check use
`-hwaccel cuda -hwaccel_output_format cuda`. NVENC sample and final encode use
the same pair when both capabilities are available. The final video stream
adds `-fps_mode:v:<ordinal> passthrough`, where `passthrough` tells FFmpeg to
preserve decoded timestamps without duplicating or dropping frames. Sample
output also uses passthrough timing so the compared interval has the same frame
cadence. `cmd/vcompress/main.go` reports `decoder=nvdec-zero-copy` when both
NVDEC and NVENC were selected, `decoder=nvdec-host-copy` when NVDEC feeds
libx265, and `decoder=software` when NVDEC is unavailable.

No new library is introduced. The minimum runtime stays an FFmpeg build with
`libx265`; NVIDIA acceleration additionally requires working `hevc_nvenc` and
CUDA support, as documented today.

## Plan of work

First add focused argument-construction tests in
`internal/ffmpeg/client_test.go`. They will prove that hardware NVENC paths add
the CUDA output format and omit the software pixel-format request, that a
software retry restores the pixel format, that x265 and SSIM paths do not ask
software filters to consume CUDA frames, and that final encode timing is
passthrough. This makes the memory-boundary contract observable without an
NVIDIA GPU.

Then revise the helper and its callers in `internal/ffmpeg/client.go`.
Hardware-frame retention will be requested only where the downstream consumer
can accept CUDA frames. The existing fallback wrapper remains the single place
that retries failed source codecs in software, preventing sample and final
paths from drifting.

Finally update `README.md` to accurately distinguish zero-copy NVDEC-to-NVENC
transcoding from CPU-dependent SSIM/libx265 work. Format and validate the code,
cross-compile Windows, run the real libx265 integration test, and perform the
available non-NVIDIA startup smoke test. A disposable encode on an NVIDIA
machine remains required to prove real GPU behavior because the development
host is Apple Silicon.

## Concrete steps

Run these commands from `/Users/jun_networks/workspace/vcompress`.

```bash
gofmt -w cmd/vcompress/main.go internal/ffmpeg/client.go internal/ffmpeg/client_test.go
mise run check
mise run build-windows
mise run test-integration
mise run build
./vcompress --dry-run /tmp/vcompress-empty-smoke
```

`mise run check`, `mise run build-windows`, `mise run test-integration` and
`mise run build` exited zero. The non-NVIDIA smoke test reported
`encoder=libx265 decoder=software`. On an NVIDIA machine, argument-level
diagnostic output or GPU monitoring should show NVDEC and NVENC active during
the final conversion without sustained host transfers between them.

## Validation and acceptance

- [x] `mise run check` passes all unit tests and `go vet`.
- [x] `mise run build-windows` compiles the Windows executable.
- [x] `mise run test-integration` proves the software fallback still produces
      structurally valid, fully decodable HEVC and preserves both publication
      modes.
- [x] Mock-runner tests prove CUDA-frame retention is limited to NVENC/null
      consumers, pixel format is restored on software retry, and final frame
      timing uses passthrough.
- [ ] On an NVIDIA machine, a disposable conversion uses NVDEC-to-NVENC
      zero-copy and produces a validated HEVC file.
- [x] The `README.md` safety policy still holds: replacement occurs only after
      structural validation, full decode and minimum savings checks pass.

## Idempotence and recovery

All test and build commands are safe to repeat. NVIDIA startup and sample
probes create unique operating-system temporary files and remove them with
deferred cleanup. Final encodes still write only to the same-directory
`.<name>.ffmpeg-compressing-*` temporary path. If zero-copy fails for a source
codec or pixel format, FFmpeg may leave a partial temporary result, but the
existing `-y` software retry overwrites only that temporary path. The processor
removes it on every return, and the source remains untouched unless all
validation and savings gates have already passed.

Reverting `internal/ffmpeg/client.go`, `internal/ffmpeg/client_test.go` and the
corresponding `README.md` text restores the host-copy NVIDIA behavior. Do not
delete or revert unrelated working-tree changes.

## Progress

- [x] (2026-08-19 20:06Z) Inspected the existing NVIDIA command paths,
      repository safety policy and prior acceleration ExecPlan.
- [x] (2026-08-19 20:06Z) Confirmed from NVIDIA's FFmpeg guidance that
      `-hwaccel cuda` without `-hwaccel_output_format cuda` copies frames to
      host memory, and identified which current consumers can accept CUDA
      frames.
- [x] (2026-08-19 20:06Z) Ran the unchanged `mise run check`; all packages and
      `go vet` passed after granting the task access to the external Go cache.
- [x] (2026-08-19 20:08Z) Added zero-copy, host-copy boundary and
      frame-timing tests, and observed them fail against the old command
      construction before implementing the change.
- [x] (2026-08-19 20:09Z) Implemented selective CUDA-frame retention with
      unchanged software retry and restored pixel-format forcing on host-frame
      paths.
- [x] (2026-08-19 20:10Z) Updated acceleration, frame-timing and startup-path
      documentation.
- [x] (2026-08-19 20:11Z) Ran formatting, checks, Windows build, integration
      and startup smoke validation.
- [x] (2026-08-19 20:13Z) Recorded evidence and completed the retrospective;
      real NVIDIA hardware validation remains explicitly open.

## Surprises and discoveries

The previous NVIDIA implementation intentionally used only `-hwaccel cuda`
so software filters and libx265 could consume decoded frames. That conservative
choice also affects the filter-free NVENC final encode, where it causes an
unnecessary GPU-to-host-to-GPU round trip for every frame.

The sandboxed baseline `mise run check` could not access
`/Users/jun_networks/Library/Caches/go-build` and failed with `operation not
permitted`. Re-running the repository task with approved cache access passed
without code changes.

A direct `go test ./internal/ffmpeg` did not use the repository's mise-managed
toolchain and attempted an unavailable Go 1.26 darwin/amd64 toolchain download.
Running `mise exec -- go test ./internal/ffmpeg` used the pinned local toolchain
and produced the expected pre-implementation failures, then passed after the
change.

## Decision log

2026-08-20 — Codex: Use CUDA output frames only for NVENC encode and null-output
decode checks. Keeping CUDA frames for SSIM or libx265 was rejected because
those consumers are software-only and would require an explicit hardware
download anyway.

2026-08-20 — Codex: Omit `-pix_fmt` only on the zero-copy attempt and restore it
on software retry. A forced software pixel format can break direct CUDA-frame
input, while the existing post-encode pixel-format validation remains the
authoritative safety check.

2026-08-20 — Codex: Do not add speculative AQ, split-frame or forced multi-pass
settings in this change. Their support and compression tradeoffs depend on GPU
generation and workload; the existing p1-p7 preset mapping, HQ tune and
encoder-matched SSIM gate already provide a safe quality contract. This plan
targets the demonstrable memory-transfer bottleneck without narrowing hardware
compatibility.

## Artifacts and notes

The unchanged baseline completed with all packages passing:

```text
[test] ok   vcompress/internal/ffmpeg (cached)
[test] ok   vcompress/internal/processor (cached)
[vet] Finished in 224.5ms
Finished in 568.9ms
```

NVIDIA's current FFmpeg guidance describes the zero-copy pair as
`-hwaccel cuda -hwaccel_output_format cuda` and states that omitting the output
format copies decoded frames to host memory. It also recommends
`-fps_mode passthrough` to preserve source timing. These semantics are restated
here so the plan is usable without an external reference.

Before implementation, the focused tests failed on the old arguments with:

```text
NVDEC runtime probe does not retain CUDA frames: ... -hwaccel cuda -i ...
NVENC sample is not zero-copy: ... -hwaccel cuda -i ... -pix_fmt yuv420p ...
hardware attempt is not zero-copy: ... -hwaccel cuda -i ... -pix_fmt:v:0 yuv420p ...
```

After implementation, `mise exec -- go test ./internal/ffmpeg` passed. The
final `mise run check` passed all packages and vet:

```text
[vet] Finished in 125.1ms
[test] ok   vcompress/internal/ffmpeg (cached)
[test] ok   vcompress/internal/processor (cached)
Finished in 206.8ms
```

`mise run build-windows` and `mise run build` both exited zero. The real
FFmpeg integration test passed `replace_source` and `keep_original`; each
converted a 2.34 MiB MPEG-4 fixture to 773.30 KiB HEVC, a 67.8% reduction,
after SSIM selection and full validation. The local startup smoke test logged:

```text
NVIDIA-DETECT: nvenc=false (...) nvdec=false (...)
START ... encoder=libx265 decoder=software ... dry_run=true
DONE total=0 converted=0 skipped=0 failed=0 saved=0 B
```

## Outcomes and retrospective

NVDEC now retains CUDA frames for the NVENC sample encode, final encode,
startup decode probe and full decode check. The libx265 and software SSIM
paths still receive host frames. A failed zero-copy attempt retries without
CUDA arguments and restores the explicit source pixel format. Both sample and
final NVENC outputs use passthrough frame synchronization, and startup logging
distinguishes zero-copy, NVDEC host-copy and software decode selections.

No codec skip, quality, structural validation, full decode, savings or file
replacement gate changed. Unit and command-construction tests, vet, host and
Windows builds, real software integration and the available non-NVIDIA smoke
test all passed. The development host has no NVIDIA GPU, so the only open
acceptance item is a disposable real-media conversion on the target NVIDIA
machine. Its startup line should report `decoder=nvdec-zero-copy`; any source
that cannot use the path will log `NVDEC-FALLBACK` and preserve the existing
safe software retry.
