package job

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"vcompress/internal/config"
	"vcompress/internal/discovery"
	"vcompress/internal/ffmpeg"
	"vcompress/internal/fsutil"
	"vcompress/internal/logging"
	"vcompress/internal/processor"
	"vcompress/internal/progress"
	"vcompress/internal/quality"
)

type Options struct {
	Console          io.Writer
	Reporter         progress.Reporter
	StopAfterCurrent func() bool
	OnResult         func(processor.Result)
}

type Summary struct {
	Total       int       `json:"total"`
	Processed   int       `json:"processed"`
	Converted   int       `json:"converted"`
	Skipped     int       `json:"skipped"`
	Failed      int       `json:"failed"`
	SavedBytes  int64     `json:"saved_bytes"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	Interrupted bool      `json:"interrupted"`
	Stopped     bool      `json:"stopped"`
}

func Run(ctx context.Context, cfg config.Config, opts Options) (summary Summary, runErr error) {
	summary = Summary{StartedAt: time.Now()}
	finish := func() { summary.FinishedAt = time.Now() }
	defer finish()

	if err := cfg.Normalize(); err != nil {
		return summary, err
	}
	targets := cfg.TargetPaths()
	logDir, err := targetLogDirectory(targets[0])
	if err != nil {
		return summary, err
	}
	if _, err := exec.LookPath(cfg.FFmpegPath); err != nil {
		return summary, fmt.Errorf("ffmpeg not found: %w", err)
	}
	if _, err := exec.LookPath(cfg.FFprobePath); err != nil {
		return summary, fmt.Errorf("ffprobe not found: %w", err)
	}

	console := opts.Console
	if console == nil {
		console = io.Discard
	}
	logPath := filepath.Join(logDir, "ffmpeg-compress.log")
	log, err := logging.New(logPath, console)
	if err != nil {
		return summary, fmt.Errorf("create log: %w", err)
	}
	defer log.Close()

	client := ffmpeg.New(cfg.FFmpegPath, cfg.FFprobePath)
	client.Logf = log.Printf
	if err := client.HasLibx265(ctx); err != nil {
		log.Printf("ERROR: %v", err)
		return summary, err
	}
	if RequiresLibvmaf(cfg) {
		if err := client.HasLibvmaf(ctx); err != nil {
			log.Printf("ERROR: %v", err)
			return summary, err
		}
	}
	client.NVIDIA = client.DetectNVIDIA(ctx)
	log.Printf("NVIDIA-DETECT: nvenc=%t (%s) nvdec=%t (%s)",
		client.NVIDIA.NVENC, client.NVIDIA.NVENCReason, client.NVIDIA.NVDEC, client.NVIDIA.NVDECReason)
	selector, encoder, qualityMode, err := BuildQualitySelector(cfg, client, log, opts.Reporter)
	if err != nil {
		log.Printf("ERROR: %v", err)
		return summary, err
	}
	decoder := "software"
	if client.NVIDIA.NVDEC {
		decoder = "nvdec-host-copy"
		if encoder == "hevc_nvenc" {
			decoder = "nvdec-zero-copy"
		}
	}
	proc := processor.Processor{Config: cfg, Media: client, Selector: selector, Logger: log, Reporter: opts.Reporter}

	log.Printf("START targets=%v quality_mode=%s encoder=%s decoder=%s preset=%s analysis_preset=%s vmaf_avg_min=%.4f vmaf_worst_min=%.4f ssim_avg_min=%.6f ssim_worst_min=%.6f sample_duration=%.3fs sample_count=%d min_savings=%.1f%% full_decode_check=%t keep_original=%t dry_run=%t",
		targets, qualityMode, encoder, decoder, cfg.Preset, cfg.AnalysisPreset, cfg.VMAFAverageMin, cfg.VMAFWorstMin, cfg.SSIMAverageMin, cfg.SSIMWorstMin, cfg.SampleDuration, cfg.SampleCount, cfg.MinSavings, cfg.FullDecodeCheck, cfg.KeepOriginal, cfg.DryRun)
	progress.Emit(opts.Reporter, progress.Event{Phase: progress.PhaseDiscovery, Message: "Discovering video candidates"})
	paths, err := discovery.ListTargets(targets)
	if err != nil {
		log.Printf("ERROR: recursive traversal failed: %v", err)
		summary.Failed++
		return summary, fmt.Errorf("recursive traversal failed: %w", err)
	}
	summary.Total = len(paths)
	progress.Emit(opts.Reporter, progress.Event{Phase: progress.PhaseDiscovery, Message: "Discovery complete", Total: summary.Total})

	for _, path := range paths {
		if ctx.Err() != nil {
			summary.Interrupted = true
			log.Printf("Interrupted. Source file was not replaced unless its validation had already completed.")
			return summary, ctx.Err()
		}
		if opts.StopAfterCurrent != nil && opts.StopAfterCurrent() {
			summary.Stopped = true
			break
		}
		result := proc.Process(ctx, path)
		summary.Processed++
		switch result.Status {
		case processor.StatusConverted:
			summary.Converted++
			summary.SavedBytes += result.SavedBytes
		case processor.StatusSkipped:
			summary.Skipped++
		case processor.StatusFailed:
			summary.Failed++
		case processor.StatusCancelled:
			summary.Interrupted = true
		}
		if opts.OnResult != nil {
			opts.OnResult(result)
		}
		if result.Status == processor.StatusCancelled || ctx.Err() != nil {
			summary.Interrupted = true
			log.Printf("Interrupted. Source file was not replaced unless its validation had already completed.")
			if err := ctx.Err(); err != nil {
				return summary, err
			}
			return summary, context.Canceled
		}
	}

	log.Printf("DONE total=%d converted=%d skipped=%d failed=%d saved=%s", summary.Total, summary.Converted, summary.Skipped, summary.Failed, fsutil.HumanSize(summary.SavedBytes))
	return summary, nil
}

func targetLogDirectory(target string) (string, error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("inspect first target %q: %w", target, err)
	}
	if info.IsDir() {
		return target, nil
	}
	if info.Mode().IsRegular() {
		return filepath.Dir(target), nil
	}
	return "", fmt.Errorf("first target is not a regular file or directory: %s", target)
}

func RequiresLibvmaf(cfg config.Config) bool {
	return cfg.DirectCRF == nil && cfg.DirectCQ == nil &&
		(cfg.QualityMetric == "vmaf" || cfg.QualityMetric == "both")
}

func BuildQualitySelector(cfg config.Config, client *ffmpeg.Client, log quality.Logger, reporter progress.Reporter) (processor.Selector, string, string, error) {
	if cfg.DirectCRF != nil {
		return quality.FixedSelector{Value: *cfg.DirectCRF, Encoder: "libx265"},
			"libx265", fmt.Sprintf("direct-crf:%d:analysis-skipped", *cfg.DirectCRF), nil
	}
	if cfg.DirectCQ != nil {
		if !client.NVIDIA.NVENC {
			return nil, "", "", fmt.Errorf("-cq requires working NVIDIA NVENC: %s", client.NVIDIA.NVENCReason)
		}
		return quality.FixedSelector{Value: *cfg.DirectCQ, Encoder: "hevc_nvenc"},
			"hevc_nvenc", fmt.Sprintf("direct-cq:%d:analysis-skipped", *cfg.DirectCQ), nil
	}

	encoder := "libx265"
	fallbackEncoder := ""
	if client.NVIDIA.NVENC {
		encoder = "hevc_nvenc"
		fallbackEncoder = "libx265"
	}
	return quality.Selector{
		Measurer: client, Logger: log, PreferredEncoder: encoder, FallbackEncoder: fallbackEncoder,
		SSIMAverageMin: cfg.SSIMAverageMin, SSIMWorstMin: cfg.SSIMWorstMin,
		VMAFAverageMin: cfg.VMAFAverageMin, VMAFWorstMin: cfg.VMAFWorstMin,
		SampleDuration: cfg.SampleDuration, SampleCount: cfg.SampleCount,
		Preset: cfg.AnalysisPreset, Metric: quality.Metric(cfg.QualityMetric), Reporter: reporter,
	}, encoder, cfg.QualityMetric + "-auto:20/18/16", nil
}

func IsInterrupted(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
