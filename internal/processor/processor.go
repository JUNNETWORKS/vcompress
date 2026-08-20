package processor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	"vcompress/internal/config"
	ff "vcompress/internal/ffmpeg"
	"vcompress/internal/fsutil"
	"vcompress/internal/media"
	"vcompress/internal/progress"
	"vcompress/internal/quality"
)

type Logger interface {
	Printf(format string, args ...any)
}

type MediaClient interface {
	Probe(ctx context.Context, path string) (media.Info, error)
	Encode(ctx context.Context, o ff.EncodeOptions) error
	FullDecode(ctx context.Context, path string, ordinal int) error
}

type Selector interface {
	Select(ctx context.Context, input string, ordinal int, pixFmt string, duration float64) (quality.Result, error)
}

type Processor struct {
	Config   config.Config
	Media    MediaClient
	Selector Selector
	Logger   Logger
	Reporter progress.Reporter
}

type Status int

const (
	StatusConverted Status = iota
	StatusSkipped
	StatusFailed
	StatusCancelled
)

type Result struct {
	Status        Status
	Path          string
	OutputPath    string
	Reason        string
	SavedBytes    int64
	OriginalBytes int64
	OutputBytes   int64
}

func (p *Processor) Process(ctx context.Context, input string) Result {
	var originalSize, outputSize int64
	outputPath := ""
	result := func(status Status, reason string) Result {
		return Result{
			Status: status, Path: input, OutputPath: outputPath, Reason: reason,
			OriginalBytes: originalSize, OutputBytes: outputSize,
		}
	}
	progress.Emit(p.Reporter, progress.Event{Phase: progress.PhaseProbe, Path: input})
	info, err := p.Media.Probe(ctx, input)
	if err != nil {
		if ctx.Err() != nil {
			return result(StatusCancelled, "cancelled during probe")
		}
		p.log("SKIP: could not probe main video: %s | %v", input, err)
		return result(StatusSkipped, "could not probe main video: "+err.Error())
	}

	switch PolicyForCodec(info.Video.CodecName) {
	case SkipEfficient:
		p.log("SKIP: already efficient codec (%s): %s", info.Video.CodecName, input)
		return result(StatusSkipped, "already efficient codec: "+info.Video.CodecName)
	case SkipArchivalOrUnknown:
		p.log("SKIP: archival/intermediate/unknown codec (%s): %s", info.Video.CodecName, input)
		return result(StatusSkipped, "archival, intermediate, or unknown codec: "+info.Video.CodecName)
	}
	if info.Video.HDR {
		p.log("SKIP: HDR/HDR-metadata detected; preserving original untouched: %s", input)
		return result(StatusSkipped, "HDR or HDR metadata detected")
	}
	if info.Duration <= 0 {
		p.log("SKIP: duration is unknown/invalid: %s", input)
		return result(StatusSkipped, "duration is unknown or invalid")
	}

	paths, err := fsutil.PlanOutput(input, p.Config.KeepOriginal)
	if err != nil {
		p.log("FAILED: cannot plan output: %s | %v", input, err)
		return result(StatusFailed, "cannot plan output: "+err.Error())
	}
	outputPath = paths.Final
	defer os.Remove(paths.Temp)

	if paths.Final != input {
		if _, err := os.Stat(paths.Final); err == nil {
			p.log("SKIP: target already exists, refusing to overwrite: %s", paths.Final)
			return result(StatusSkipped, "target already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			p.log("FAILED: cannot inspect target path: %s | %v", paths.Final, err)
			return result(StatusFailed, "cannot inspect target path: "+err.Error())
		}
	}

	st, err := os.Stat(input)
	if err != nil {
		p.log("FAILED: stat source: %s | %v", input, err)
		return result(StatusFailed, "cannot stat source: "+err.Error())
	}
	originalSize = st.Size()
	p.log("PROCESS: %s | codec=%s pix_fmt=%s %dx%d fps=%s bitrate=%d size=%s",
		input, info.Video.CodecName, info.Video.PixFmt, info.Video.Width, info.Video.Height,
		info.Video.AvgFrameRate, info.Video.BitRate, fsutil.HumanSize(originalSize))

	progress.Emit(p.Reporter, progress.Event{Phase: progress.PhaseQuality, Path: input})
	selected, err := p.Selector.Select(ctx, input, info.Video.Ordinal, info.Video.PixFmt, info.Duration)
	if err != nil {
		if ctx.Err() != nil {
			return result(StatusCancelled, "cancelled during quality analysis")
		}
		p.log("FAILED: quality analysis could not be completed safely: %s | %v", input, err)
		return result(StatusFailed, "quality analysis failed: "+err.Error())
	}
	if !selected.Found {
		p.log("KEEP ORIGINAL: even quality value 16 did not meet %s thresholds: %s", selected.Metric, input)
		return result(StatusSkipped, "quality thresholds were not met")
	}
	qualitySummary := describeQuality(selected)
	if selected.Compared {
		p.log("QUALITY-SELECT: %s", qualitySummary)
	} else {
		p.log("QUALITY-DIRECT: %s", qualitySummary)
	}

	if p.Config.DryRun {
		p.log("DRY-RUN: selected %s; would encode to %s", qualitySummary, paths.Final)
		return result(StatusSkipped, "dry run")
	}

	progress.Emit(p.Reporter, progress.Event{Phase: progress.PhaseEncode, Path: input, TotalSeconds: info.Duration})
	if err := p.Media.Encode(ctx, ff.EncodeOptions{
		Input:      input,
		Output:     paths.Temp,
		Ordinal:    info.Video.Ordinal,
		PixFmt:     info.Video.PixFmt,
		Preset:     p.Config.Preset,
		Quality:    selected.Value,
		ColorRange: info.Video.ColorRange,
		ColorSpace: info.Video.ColorSpace,
		ColorTrc:   info.Video.ColorTransfer,
		ColorPrim:  info.Video.ColorPrimaries,
		Encoder:    selected.Encoder,
		Duration:   info.Duration,
		Progress: func(update ff.EncodeProgress) {
			progress.Emit(p.Reporter, progress.Event{
				Phase: progress.PhaseEncode, Path: input,
				ProcessedSeconds: update.ProcessedSeconds, TotalSeconds: update.TotalSeconds,
				Percent: update.Percent, ETASeconds: update.ETASeconds, Speed: update.Speed,
			})
		},
	}); err != nil {
		if ctx.Err() != nil {
			return result(StatusCancelled, "cancelled during encode")
		}
		p.log("FAILED: ffmpeg encode/mux failed: %s | %v", input, err)
		return result(StatusFailed, "ffmpeg encode or mux failed: "+err.Error())
	}
	if ctx.Err() != nil {
		return result(StatusCancelled, "cancelled after encode")
	}

	progress.Emit(p.Reporter, progress.Event{Phase: progress.PhaseValidation, Path: input})
	if err := p.validate(ctx, paths.Temp, info); err != nil {
		if ctx.Err() != nil {
			return result(StatusCancelled, "cancelled during validation")
		}
		p.log("VALIDATION FAILED: %s | %v", input, err)
		return result(StatusFailed, "validation failed: "+err.Error())
	}

	outStat, err := os.Stat(paths.Temp)
	if err != nil {
		p.log("FAILED: stat output: %s | %v", paths.Temp, err)
		return result(StatusFailed, "cannot stat output: "+err.Error())
	}
	outputSize = outStat.Size()
	if outputSize >= originalSize {
		p.log("KEEP ORIGINAL: output is not smaller (%s -> %s)", fsutil.HumanSize(originalSize), fsutil.HumanSize(outputSize))
		return result(StatusSkipped, "output is not smaller")
	}
	pct := fsutil.SavingsPercent(originalSize, outputSize)
	if pct < p.Config.MinSavings {
		p.log("KEEP ORIGINAL: only %.1f%% smaller; threshold is %.1f%%", pct, p.Config.MinSavings)
		return result(StatusSkipped, "output did not meet the minimum savings threshold")
	}
	if ctx.Err() != nil {
		return result(StatusCancelled, "cancelled before publication")
	}

	_ = os.Chmod(paths.Temp, st.Mode().Perm())
	_ = os.Chtimes(paths.Temp, st.ModTime(), st.ModTime())
	progress.Emit(p.Reporter, progress.Event{Phase: progress.PhasePublish, Path: input})

	if paths.Final == input {
		if err := fsutil.ReplaceFile(paths.Temp, input); err != nil {
			p.log("FAILED: could not replace source: %s | %v", input, err)
			return result(StatusFailed, "could not replace source: "+err.Error())
		}
	} else {
		if err := os.Rename(paths.Temp, paths.Final); err != nil {
			p.log("FAILED: could not publish validated output: %s | %v", paths.Final, err)
			return result(StatusFailed, "could not publish validated output: "+err.Error())
		}
		if !p.Config.KeepOriginal {
			if err := os.Remove(input); err != nil {
				p.log("WARNING: output created but source could not be removed; both files remain: %s | %s", input, paths.Final)
				return result(StatusFailed, "output published but source could not be removed: "+err.Error())
			}
		}
	}

	saved := originalSize - outputSize
	if p.Config.KeepOriginal {
		p.log("OK: %s | %s -> %s | potential_saving=%s (%.1f%%) | source retained=%s | %s",
			qualitySummary,
			fsutil.HumanSize(originalSize), fsutil.HumanSize(outputSize), fsutil.HumanSize(saved), pct, input, paths.Final)
		return result(StatusConverted, "converted; source retained")
	}
	p.log("OK: %s | %s -> %s | saved=%s (%.1f%%) | %s",
		qualitySummary,
		fsutil.HumanSize(originalSize), fsutil.HumanSize(outputSize), fsutil.HumanSize(saved), pct, paths.Final)
	completed := result(StatusConverted, "converted")
	completed.SavedBytes = saved
	return completed
}

func (s Status) String() string {
	switch s {
	case StatusConverted:
		return "converted"
	case StatusSkipped:
		return "skipped"
	case StatusFailed:
		return "failed"
	case StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

func describeQuality(selected quality.Result) string {
	if selected.Compared {
		switch selected.Metric {
		case quality.MetricVMAF:
			return fmt.Sprintf("encoder=%s value=%d avg_vmaf=%.4f worst_vmaf=%.4f",
				selected.Encoder, selected.Value, selected.VMAFAverage, selected.VMAFWorst)
		case quality.MetricSSIM:
			return fmt.Sprintf("encoder=%s value=%d avg_ssim=%.6f worst_ssim=%.6f",
				selected.Encoder, selected.Value, selected.SSIMAverage, selected.SSIMWorst)
		default:
			return fmt.Sprintf("encoder=%s value=%d avg_vmaf=%.4f worst_vmaf=%.4f avg_ssim=%.6f worst_ssim=%.6f",
				selected.Encoder, selected.Value, selected.VMAFAverage, selected.VMAFWorst, selected.SSIMAverage, selected.SSIMWorst)
		}
	}
	name := "crf"
	if selected.Encoder == "hevc_nvenc" {
		name = "cq"
	}
	return fmt.Sprintf("encoder=%s %s=%d analysis=skipped", selected.Encoder, name, selected.Value)
}

func (p *Processor) validate(ctx context.Context, out string, in media.Info) error {
	got, err := p.Media.Probe(ctx, out)
	if err != nil {
		return fmt.Errorf("ffprobe cannot read output: %w", err)
	}
	if !reflect.DeepEqual(in.StreamCounts, got.StreamCounts) {
		return fmt.Errorf("stream-type counts changed: %v -> %v", in.StreamCounts, got.StreamCounts)
	}
	if in.ChapterCount != got.ChapterCount {
		return fmt.Errorf("chapter count changed: %d -> %d", in.ChapterCount, got.ChapterCount)
	}
	if !media.DurationClose(in.Duration, got.Duration) {
		return fmt.Errorf("duration changed too much: %.3f -> %.3f", in.Duration, got.Duration)
	}
	if got.Video.CodecName != "hevc" {
		return fmt.Errorf("output codec is %s, expected hevc", got.Video.CodecName)
	}
	if in.Video.PixFmt != got.Video.PixFmt {
		return fmt.Errorf("pixel format changed: %s -> %s", in.Video.PixFmt, got.Video.PixFmt)
	}
	if in.Video.Width != got.Video.Width || in.Video.Height != got.Video.Height {
		return fmt.Errorf("resolution changed: %dx%d -> %dx%d", in.Video.Width, in.Video.Height, got.Video.Width, got.Video.Height)
	}
	if !media.FPSClose(in.Video.AvgFrameRate, got.Video.AvgFrameRate) {
		return fmt.Errorf("frame rate changed: %s -> %s", in.Video.AvgFrameRate, got.Video.AvgFrameRate)
	}
	if !media.SameOrUnknown(in.Video.ColorRange, got.Video.ColorRange) {
		return fmt.Errorf("color_range changed: %s -> %s", in.Video.ColorRange, got.Video.ColorRange)
	}
	if !media.SameOrUnknown(in.Video.ColorSpace, got.Video.ColorSpace) {
		return fmt.Errorf("color_space changed: %s -> %s", in.Video.ColorSpace, got.Video.ColorSpace)
	}
	if !media.SameOrUnknown(in.Video.ColorTransfer, got.Video.ColorTransfer) {
		return fmt.Errorf("color_transfer changed: %s -> %s", in.Video.ColorTransfer, got.Video.ColorTransfer)
	}
	if !media.SameOrUnknown(in.Video.ColorPrimaries, got.Video.ColorPrimaries) {
		return fmt.Errorf("color_primaries changed: %s -> %s", in.Video.ColorPrimaries, got.Video.ColorPrimaries)
	}
	if p.Config.FullDecodeCheck {
		if err := p.Media.FullDecode(ctx, out, in.Video.Ordinal); err != nil {
			return err
		}
	}
	return nil
}

func (p *Processor) log(format string, args ...any) {
	if p.Logger != nil {
		p.Logger.Printf(format, args...)
	}
}
