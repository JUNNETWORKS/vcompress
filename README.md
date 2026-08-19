# vcompress

Cross-platform (Windows/Linux/macOS) recursive video compressor built around FFmpeg/ffprobe.
It converts selected legacy/delivery codecs to HEVC/x265 only when a short, representative SSIM analysis chooses an acceptable CRF from **20 → 18 → 16**, validates the finished output, and confirms that the file is meaningfully smaller.

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
- Uses a same-directory temporary output. Unix replacement (Linux/macOS) is an atomic rename; Windows replacement uses `MoveFileExW` with `MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH`.
- With `--keep-original`, publishes the validated output beside the source without replacing or deleting the source. Same-container output uses a `.hevc` suffix, such as `movie.hevc.mp4`.
- Keeps the container for `.mp4`, `.m4v`, `.mov` and `.mkv`; any other input container is remuxed to `.mkv` next to the source, and the source is only removed after the new file has passed every check. If that `.mkv` path already exists, the file is skipped instead of overwritten.
- Processing is intentionally sequential so one x265 encode can use the machine without multiple simultaneous encodes exhausting CPU/RAM.

## Requirements

- Go 1.26 to build (`mise` installs and pins it; `GOTOOLCHAIN=local` keeps builds identical to CI).
- `ffmpeg` and `ffprobe` available in `PATH` at runtime.
- FFmpeg must include the `libx265` encoder.

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

Important options:

```text
-preset slow
-analysis-preset slow
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

The log is written to `ffmpeg-compress.log` in the selected root directory.
With `-keep-original`, `movie.mp4` produces `movie.hevc.mp4` and leaves
`movie.mp4` unchanged. Inputs that require a container change, such as
`movie.avi`, produce `movie.mkv` and likewise retain the source. An existing
destination is never overwritten.

## Automatic CRF selection

For each eligible file:

1. Representative sample positions are spread through the video.
2. CRF 20 is sample-encoded with x265 and x265's `SSIM Mean Y` is captured.
3. CRF 20 is accepted when both average and worst-sample thresholds pass.
4. Otherwise CRF 18 is tested, then CRF 16.
5. If none pass, the source is left unchanged.
6. A complete encode is performed only after a CRF is selected.
7. The complete output must pass structural/full-decode checks and the minimum size saving before publication.

SSIM is an objective proxy, not a mathematical guarantee of perceptual transparency.

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
internal/quality/       representative sampling and CRF 20/18/16 selection
internal/processor/     per-file orchestration and safety gates
internal/logging/       console + file logger
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
