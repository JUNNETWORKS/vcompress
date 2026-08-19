# NVIDIA NVENC and NVDEC hardware acceleration

## Purpose and big picture

When `vcompress` starts on a machine whose FFmpeg build and NVIDIA driver can
actually use NVIDIA's video hardware, it will use NVENC (NVIDIA's hardware
encoder) for HEVC output and NVDEC (NVIDIA's hardware decoder) while reading
video. Capability names in an FFmpeg listing are not enough: startup probes
must execute a tiny encode and decode so a build that advertises CUDA but has
no usable device continues with the current `libx265` and software-decoding
behaviour. A source codec that NVDEC cannot decode must also retry with the
software decoder without weakening any output validation or replacement gate.

NVENC output must be assessed using samples encoded by NVENC itself. Reusing
an x265 quality score for a different encoder is out of scope because equal
numeric quality controls do not promise equal output between encoders. GPU
selection flags and support for non-NVIDIA hardware are also out of scope.

## Context and orientation

`cmd/vcompress/main.go` validates the FFmpeg installation, constructs the
shared FFmpeg client and the quality selector, and writes the startup log.
`internal/ffmpeg/client.go` probes media, performs sample and final encodes,
and performs the full decode check through the process abstraction in
`internal/ffmpeg/runner.go`. `internal/quality/selector.go` tests quality
control values 20, 18 and 16 against average and worst-sample Structural
Similarity Index Measure (SSIM) thresholds. `internal/processor/processor.go`
coordinates one source file, validates the temporary result and only then
publishes it. `README.md` describes the runtime requirements and safety
contract.

NVENC is NVIDIA's dedicated hardware video encoder; `hevc_nvenc` is FFmpeg's
HEVC encoder backed by it. NVDEC is NVIDIA's dedicated video decoder; FFmpeg's
`-hwaccel cuda` input option requests it and returns decoded frames to system
memory when `-hwaccel_output_format cuda` is not requested. This permits the
existing software filters and either encoder to consume those frames. CQ is
NVENC's constant-quality target. It is not x265's Constant Rate Factor (CRF),
so the same candidate numbers are only candidate controls and must be measured
from the actual encoder's reconstructed result. A fallback is a repeat of the
same operation without the failed hardware path; it must never publish a
partial temporary file.

## Interfaces and dependencies

`internal/ffmpeg/client.go` will add an `NVIDIACapabilities` value containing
independent `NVENC` and `NVDEC` booleans plus diagnostic reasons, and
`(*Client).DetectNVIDIA(context.Context) NVIDIACapabilities`. Detection first
checks the FFmpeg feature listings, then runs a one-frame `hevc_nvenc` encode
for NVENC and creates a tiny libx265 HEVC file followed by a CUDA-accelerated
decode for NVDEC. The temporary probe file is created in the operating
system's temporary directory and always removed.

The client will retain the selected capabilities and expose an optional log
callback. Sample measurement will accept an encoder name. `libx265` keeps its
current internal SSIM reporting. `hevc_nvenc` writes a short Matroska sample
using `-preset pN -tune hq -rc vbr -cq N -b:v 0`; a second FFmpeg invocation
compares it with the same source interval using the `ssim` filter. The final
encode uses the identical encoder family and quality options. Existing x265
preset names map monotonically from `ultrafast` through `placebo` onto NVENC
presets `p1` through `p7`.

`internal/quality/selector.go` will carry a preferred encoder and an optional
fallback encoder. It will restart the entire candidate search with `libx265`
if NVENC cannot encode a sample, so every score used for a decision comes from
one encoder. Its result will name that encoder. `internal/processor/processor.go`
will pass the selected encoder into the final encode. NVDEC failures will be
retried without `-hwaccel cuda` inside the FFmpeg client; an NVENC final-encode
failure after successful NVENC analysis remains a failed file, leaving the
source untouched, rather than silently publishing output from an encoder that
was not measured.

No new external library is introduced. FFmpeg still must provide `libx265`,
because it is the mandatory fallback and is also used to create the NVDEC
startup probe stream.

## Plan of work

First add capability detection and focused argument helpers to
`internal/ffmpeg/client.go`, with mocked-runner tests that distinguish advertised
support from a working runtime device. Then add NVENC sample generation and
filter-based SSIM parsing while retaining the existing x265 path. Thread the
chosen encoder through `internal/quality/selector.go` and
`internal/processor/processor.go` so analysis and final output cannot diverge.
After that, initialize and log the detected capabilities in
`cmd/vcompress/main.go`, update runtime documentation, and run unit, vet,
cross-platform compile and real software integration checks. If this host has
no NVIDIA device, record the observed fallback; GPU acceptance must then be
verified on an NVIDIA host using the documented dry run and a disposable
copy, never an irreplaceable source.

## Concrete steps

Run all commands from `/Users/jun_networks/workspace/vcompress`.

```bash
gofmt -w internal/ffmpeg/client.go internal/ffmpeg/client_test.go internal/quality/selector.go internal/quality/selector_test.go internal/processor/processor.go internal/processor/processor_test.go cmd/vcompress/main.go
mise run check
mise run build-windows
mise run test-integration
./vcompress --dry-run /path/to/disposable/videos
```

The check and Windows build must exit zero. On a host without usable NVIDIA
video hardware, the startup log should say both accelerators are unavailable
and the integration transcript should continue to report successful HEVC
conversion via libx265. On an NVIDIA host, the log should select
`encoder=hevc_nvenc` and `decoder=nvdec`; NVENC sample scores should precede an
NVENC final encode.

## Validation and acceptance

- [x] `mise run check` passes all unit tests and `go vet`.
- [x] `mise run build-windows` compiles the Windows path.
- [x] `mise run test-integration` proves the existing libx265 fallback path.
- [x] Mocked FFmpeg tests prove that list-only support is rejected when a
      runtime probe fails, NVDEC commands retry in software, and NVENC analysis
      and final encoding receive their matching options.
- [ ] On an NVIDIA machine, a dry run and a disposable real conversion log
      NVENC/NVDEC selection and produce validated HEVC.
- [x] The `README.md` safety policy still holds: source files are only replaced
      after structural validation, the configured full decode check and the
      minimum savings gate pass.

## Idempotence and recovery

Startup probes are read-only except for a uniquely named file in the operating
system temporary directory, which is removed by a deferred cleanup. Repeating
detection or tests is safe. Sample outputs also use unique temporary files and
are removed after comparison. Final encodes continue to target
`.<name>.ffmpeg-compressing-*` beside the source with `-y`; a retry overwrites
only that planned temporary path. `internal/processor/processor.go` removes it
on every return. An interrupted or failed hardware operation therefore leaves
the source and any existing final destination untouched. Revert the files in
this plan to restore the software-only behaviour; do not remove unrelated
working-tree changes.

## Progress

- [x] (2026-08-19 18:51Z) Inspected the existing FFmpeg, quality-selection,
      processing, CLI, test and safety-policy paths and wrote this ExecPlan.
- [x] (2026-08-19 18:54Z) Added executable NVENC/NVDEC detection and mocked
      success plus advertised-but-unusable tests.
- [x] (2026-08-19 18:55Z) Added encoder-matched SSIM measurement and NVDEC
      retry behaviour.
- [x] (2026-08-19 18:55Z) Threaded the selected encoder through quality
      selection and final encoding.
- [x] (2026-08-19 18:56Z) Initialized startup/fallback logging and updated
      runtime documentation.
- [x] (2026-08-19 18:58Z) Ran formatting, unit tests, vet, Windows compilation,
      the real FFmpeg integration test and a no-NVIDIA CLI smoke test.
- [x] (2026-08-19 18:58Z) Recorded evidence and completed the retrospective.

## Surprises and discoveries

The existing sample path obtains SSIM from x265's private log output rather
than FFmpeg's general `ssim` filter. Therefore a final-only NVENC substitution
would violate the assumption that the selected quality value describes the
encoder which creates the published file.

The available development host is Apple Silicon and its Homebrew FFmpeg 7.1.1
does not expose `hevc_nvenc` or CUDA. The real startup smoke test therefore
proved the unchanged software selection but could not execute NVIDIA hardware.
The NVIDIA command construction, runtime-probe decisions and fallback paths
are covered with mocked process tests; a real NVIDIA-host acceptance run
remains operational follow-up.

## Decision log

2026-08-20 — Codex: Detect runtime usability with real one-frame operations,
not only `ffmpeg -encoders`, `ffmpeg -decoders` or `ffmpeg -hwaccels` text.
Packaged FFmpeg can advertise NVIDIA components while the device, driver or
container device mapping is unavailable.

2026-08-20 — Codex: Measure NVENC output through FFmpeg's SSIM filter and carry
the chosen encoder into final encoding. Reusing x265 SSIM or treating x265 CRF
and NVENC CQ as equivalent was rejected because that would make the existing
quality threshold misleading.

2026-08-20 — Codex: Request CUDA decoding without CUDA output frames and retry
the operation with software decoding on failure. This uses NVDEC where the
input codec supports it while preserving compatibility with software filters,
pixel formats and legacy codecs that a particular GPU cannot decode.

## Artifacts and notes

Initial inspection found the current final codec fixed at `libx265`, sample
quality parsed from `SSIM Mean Y`, and the default full decode command using no
hardware acceleration. The working tree was clean before implementation.

`mise run check` completed with all packages passing and `go vet` clean.
`mise run build-windows` completed with exit status zero. `mise run
test-integration` converted both replace-source and keep-original MPEG-4
fixtures through libx265, preserving validation: both outputs were 67.8%
smaller and both subtests passed.

The startup smoke test on the available host logged:

```text
NVIDIA-DETECT: nvenc=false (hevc_nvenc is not present in this FFmpeg build) nvdec=false (CUDA hardware acceleration is not present in this FFmpeg build)
START ... encoder=libx265 decoder=software ... dry_run=true
DONE total=0 converted=0 skipped=0 failed=0 saved=0 B
```

The tests in `internal/ffmpeg/client_test.go` exercise successful runtime
probes, rejection of unusable advertised NVENC and NVDEC, NVENC sample SSIM,
and software-decode retry without changing the selected NVENC encoder. The
test in `internal/quality/selector_test.go` proves that an NVENC analysis
failure restarts the whole decision with libx265.

## Outcomes and retrospective

The CLI now performs executable NVIDIA probes, selects NVENC and NVDEC
independently, measures NVENC's own output before selecting CQ, and carries the
measured encoder into the final encode. NVDEC failures retry the same operation
with software decoding. NVENC sample incompatibility restarts quality analysis
with libx265, while an unexpected final NVENC failure preserves the source and
reports failure rather than publishing unmeasured output. The existing
structural checks, full decode check, savings gate and replacement functions
were not weakened.

All locally available automated and real-FFmpeg checks passed. Because this
development host has no NVIDIA GPU, the only open validation item is a real
NVIDIA-host dry run and disposable conversion; no implementation change is
known to be pending.
