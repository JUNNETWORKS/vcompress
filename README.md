# vcompress

Cross-platform (Windows/Linux/macOS) recursive video compressor built around FFmpeg/ffprobe.
By default, it converts selected legacy/delivery codecs to HEVC only when a short, representative VMAF analysis chooses an acceptable quality value from **20 → 18 → 16**, validates the finished output, and confirms that the file is meaningfully smaller. SSIM can be selected instead, or both metrics can be required. For large collections where the analysis time is undesirable, an explicit CRF or NVENC CQ can bypass only the quality-selection step. It uses NVIDIA NVENC and NVDEC automatically when runtime probes confirm that they work, keeps NVDEC frames in GPU memory when passing them directly to NVENC, and otherwise keeps the existing libx265/software path.

## Safety policy

- Recursively discovers common video extensions using Go's `filepath.WalkDir`.
- Skips HEVC, AV1, VP9 and VVC to avoid needless generational loss.
- Skips ProRes, DNxHD, FFV1, raw video and other mastering/lossless/unknown codecs.
- Skips PQ/HLG and HDR-related side metadata, including Dolby Vision/HDR10+-style metadata.
- Preserves resolution, frame rate, pixel format/chroma/bit depth and explicit color signalling.
- Stream-copies audio, subtitles, attachments/data, chapters and metadata.
- Validates stream-type counts, chapters, duration, codec, pixel format, resolution, FPS and color signalling.
- Performs a full decode check by default.
- Keeps the source unless the result is at least 5% smaller by default.
- `-crf` or `-cq` can explicitly bypass the pre-encode quality comparison; all
  structural, full-decode, savings and replacement checks still apply.
- Uses a same-directory temporary output. Unix replacement (Linux/macOS) is an atomic rename; Windows replacement uses `MoveFileExW` with `MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH`.
- With `--keep-original`, publishes the validated output beside the source without replacing or deleting the source. Same-container output uses a `.hevc` suffix, such as `movie.hevc.mp4`.
- Keeps the container for `.mp4`, `.m4v`, `.mov` and `.mkv`; any other input container is remuxed to `.mkv` next to the source, and the source is only removed after the new file has passed every check. If that `.mkv` path already exists, the file is skipped instead of overwritten.
- Processing is intentionally sequential so one encode can use the machine without multiple simultaneous encodes exhausting CPU, GPU, RAM or VRAM.
- At startup, performs real one-frame NVIDIA encode and decode probes rather than trusting FFmpeg's feature lists alone. NVDEC-to-NVENC sample and final encodes retain CUDA frames in GPU memory instead of copying them through system memory. A source codec or pixel format that cannot use that path is retried with software decoding, and NVENC sample analysis falls back as a whole to libx265 if that format cannot be encoded by NVENC.
- Preserves decoded frame timestamps with FFmpeg's passthrough frame-sync mode so variable-frame-rate input is not intentionally duplicated or dropped during encoding.

## Requirements

- Go 1.26 to build (`mise` installs and pins it; `GOTOOLCHAIN=local` keeps builds identical to CI).
- `ffmpeg` and `ffprobe` available in `PATH` at runtime.
- FFmpeg must include the `libx265` encoder.
- Automatic `vmaf` and `both` modes require FFmpeg's `libvmaf` filter. Use
  `ffmpeg -filters` to check for it. Explicit `ssim`, `-crf`, and `-cq` modes do
  not require libvmaf.
- NVIDIA acceleration is optional. When available, FFmpeg must expose a working `hevc_nvenc` encoder and CUDA hardware acceleration, and the installed NVIDIA driver must be compatible with that FFmpeg build. `libx265` remains required as the safe fallback.

## Download

Every green push to `main` republishes the `latest` pre-release with binaries built by CI:

```text
vcompress_linux_amd64
vcompress_linux_arm64
vcompress_windows_amd64.exe
vcompress_darwin_arm64
SHA256SUMS.txt
```

The `latest` tag is mutable, so it always points at the newest `main` commit. Verify a download with `sha256sum -c SHA256SUMS.txt`. FFmpeg is still a runtime requirement; it is not bundled.

## Build

This repository uses [`mise`](https://mise.jdx.dev/) (see `mise.toml`), which pins the Go toolchain and provides tasks that mirror CI:

```bash
mise run build            # host binary
mise run build-windows    # GOOS=windows GOARCH=amd64
mise run dist             # all release artifacts into dist/
```

The equivalent plain `go` commands are below.

Linux/macOS:

```bash
go build -o vcompress ./cmd/vcompress
```

Windows PowerShell:

```powershell
go build -o vcompress.exe ./cmd/vcompress
```

Cross-compile a Windows binary from Linux/macOS:

```bash
GOOS=windows GOARCH=amd64 go build -o vcompress.exe ./cmd/vcompress
```

## Usage

Linux/macOS:

```bash
./vcompress --dry-run /path/to/videos
./vcompress /path/to/videos
```

Windows PowerShell:

```powershell
.\vcompress.exe --dry-run 'D:\Videos'
.\vcompress.exe 'D:\Videos'
```

For a local WebUI instead of entering compression options on the command line:

```bash
./vcompress web
```

The server listens only on `127.0.0.1:8080` and opens the default browser.
Use `vcompress web --no-open` to print the URL without opening it, or
`vcompress web --port 18080` to select another localhost port. The page can
browse server-side directories, select multiple directories and individual
video files, configure the same quality and safety options, show candidate and
remaining file counts, follow quality-analysis and encode progress, estimate
completion time, and display per-file results. Overlapping selections are
deduplicated before processing.

Only one job runs at a time, and files remain sequential within that job. The
page offers immediate cancellation, which removes unpublished temporary output,
and a graceful stop after the current file has completed validation and safe
publication. Closing or reloading the browser does not stop processing; the
page reconnects to the in-memory job state. Job history is not persisted across
server restarts. The WebUI has no authentication because it is deliberately
localhost-only; do not proxy or otherwise expose it to a network.

Important options:

```text
-crf 20
-cq 20
-preset slow
-analysis-preset slow
-quality-metric vmaf
-vmaf-average 95
-vmaf-worst 90
-ssim-average 0.995
-ssim-worst 0.992
-sample-duration 4
-sample-count 3
-min-savings 5
-no-full-decode-check
-keep-original
-dry-run
-ffmpeg <path>
-ffprobe <path>
```

The log is written to `ffmpeg-compress.log` in the first selected directory, or
beside the first selected file when that target is a file.
`-crf N` forces libx265 CRF `N`; `-cq N` requires working NVIDIA NVENC and
uses CQ `N`. Both accept integers from 0 through 51, cannot be combined, and
skip representative sample encoding and quality comparison. Lower values usually
retain more detail and produce larger files. CRF and CQ values are not
quality-equivalent, so `-cq` never silently falls back to libx265. With neither
option, automatic VMAF selection remains enabled.

`-quality-metric` accepts `vmaf`, `ssim`, or `both`. The default `vmaf` mode
requires average and worst representative-sample scores of 95 and 90. `ssim`
uses the existing 0.995 average and 0.992 worst-sample thresholds. `both`
accepts a quality value only when both sets of thresholds pass. Thresholds are
configurable with `-vmaf-average`, `-vmaf-worst`, `-ssim-average`, and
`-ssim-worst`; the worst threshold cannot exceed its average threshold.

With `-keep-original`, `movie.mp4` produces `movie.hevc.mp4` and leaves
`movie.mp4` unchanged. Inputs that require a container change, such as
`movie.avi`, produce `movie.mkv` and likewise retain the source. An existing
destination is never overwritten. The startup line reports
`decoder=nvdec-zero-copy` when both NVIDIA engines are active;
`NVDEC-FALLBACK` records any file that must retry with software decoding.

## Automatic quality selection

For each eligible file:

1. Representative sample positions are spread through the video.
2. Quality value 20 is sample-encoded with the selected encoder. The encoded sample and matching source interval are decoded and compared with FFmpeg's `libvmaf` filter by default. `ssim` uses FFmpeg's `ssim` filter, while `both` evaluates the same encoded sample with both filters. NVDEC-to-NVENC sample encoding stays in GPU memory, while metric comparisons download decoded frames because both filters run in software.
3. Quality value 20 is accepted when both average and worst-sample thresholds pass.
4. Otherwise quality value 18 is tested, then 16. These are x265 CRF values or NVENC CQ values; they are measured independently and are not assumed to be equivalent.
5. If none pass, the source is left unchanged.
6. A complete encode is performed only after a quality value is selected.
7. The complete output must pass structural/full-decode checks and the minimum size saving before publication.

VMAF and SSIM are objective proxies, not mathematical guarantees of perceptual transparency. The default VMAF model is `vmaf_v0.6.1`, supplied by libvmaf through FFmpeg.

## Direct quality selection

To skip automatic quality analysis and use libx265 CRF 20 for every eligible file:

```bash
./vcompress -crf 20 /path/to/videos
```

To use NVIDIA NVENC CQ 20 instead:

```bash
./vcompress -cq 20 /path/to/videos
```

Direct mode is an explicit quality-policy override. It saves the sample-analysis
work, but it does not skip the finished-file probe, full decode check, minimum
savings requirement, or safe replacement process. Use `-dry-run` first to
confirm eligibility and the selected encoder without writing output.

## Tests

What CI gates on, via `mise`:

```bash
mise run check            # go test ./... plus go vet ./...
mise run test-integration # real FFmpeg/libx265 round trip
```

Unit tests (no FFmpeg media generation required):

```bash
go test ./...
```

Static analysis:

```bash
go vet ./...
```

Integration test using real FFmpeg/libx265:

```bash
go test -tags=integration ./internal/processor -run TestIntegrationMPEG4ToHEVC -v
```

Windows compile check from another OS:

```bash
GOOS=windows GOARCH=amd64 go build ./cmd/vcompress
```

## Package layout

```text
cmd/vcompress/          CLI and signal handling
internal/config/        configuration/defaults/validation
internal/discovery/     recursive cross-platform file discovery
internal/ffmpeg/        ffmpeg/ffprobe process adapter and JSON parsing
internal/fsutil/        output naming, size helpers, OS-specific safe replacement
internal/media/         media model and validation helpers
internal/quality/       representative sampling and quality 20/18/16 selection
internal/processor/     per-file orchestration and safety gates
internal/logging/       console + file logger
internal/job/           shared CLI/Web batch orchestration and counters
internal/progress/      typed processing and FFmpeg progress events
internal/webui/         localhost server, single-job state and embedded UI
```

## Development plans (ExecPlans)

Complex features and significant refactors are designed as **ExecPlans**:
self-contained, living design documents that carry the work from design through
implementation to retrospective.

- `.agent/PLANS.md` defines the format, the required sections and the rules.
- `.agent/plans/TEMPLATE.md` is the starting point; plans land in
  `.agent/plans/YYYY-MM-DD-short-slug.md` and are committed.
- `AGENTS.md` points coding agents at the same workflow. With Claude Code,
  `/execplan <task>` writes a plan and `/execplan-run [path]` executes or
  resumes one.

Small, self-contained changes do not need a plan.

## CI

`.github/workflows/ci.yml` runs unit tests and builds on both Ubuntu and Windows. A separate Ubuntu job runs `go vet` plus the real FFmpeg/libx265 integration test.

A third job publishes the `latest` pre-release, but only for pushes to `main` and only after both test jobs pass. It cross-compiles with `scripts/build-dist.sh`, the same script `mise run dist` uses locally.
