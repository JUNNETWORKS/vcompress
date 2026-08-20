package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"vcompress/internal/config"
	"vcompress/internal/discovery"
	"vcompress/internal/ffmpeg"
	"vcompress/internal/fsutil"
	"vcompress/internal/logging"
	"vcompress/internal/processor"
	"vcompress/internal/quality"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg := config.Default()
	noFullDecode := false

	flag.StringVar(&cfg.Preset, "preset", cfg.Preset, "final encoder preset (x265 name; mapped to NVENC p1-p7)")
	flag.StringVar(&cfg.AnalysisPreset, "analysis-preset", "", "sample encoder preset (default: same as -preset)")
	flag.Func("crf", "use libx265 CRF directly (0-51) and skip quality analysis", setOptionalInt(&cfg.DirectCRF))
	flag.Func("cq", "use NVENC CQ directly (0-51) and skip quality analysis", setOptionalInt(&cfg.DirectCQ))
	flag.StringVar(&cfg.QualityMetric, "quality-metric", cfg.QualityMetric, "automatic quality metric: vmaf, ssim, or both")
	flag.Float64Var(&cfg.VMAFAverageMin, "vmaf-average", cfg.VMAFAverageMin, "minimum average VMAF score")
	flag.Float64Var(&cfg.VMAFWorstMin, "vmaf-worst", cfg.VMAFWorstMin, "minimum worst-sample VMAF score")
	flag.Float64Var(&cfg.SSIMAverageMin, "ssim-average", cfg.SSIMAverageMin, "minimum average SSIM Mean Y")
	flag.Float64Var(&cfg.SSIMWorstMin, "ssim-worst", cfg.SSIMWorstMin, "minimum worst-sample SSIM Mean Y")
	flag.Float64Var(&cfg.SampleDuration, "sample-duration", cfg.SampleDuration, "seconds per representative sample")
	flag.IntVar(&cfg.SampleCount, "sample-count", cfg.SampleCount, "number of representative samples (1-5)")
	flag.Float64Var(&cfg.MinSavings, "min-savings", cfg.MinSavings, "minimum size reduction percent required for replacement")
	flag.BoolVar(&noFullDecode, "no-full-decode-check", false, "skip full decode verification of generated output")
	flag.BoolVar(&cfg.KeepOriginal, "keep-original", cfg.KeepOriginal, "publish compressed output beside the source without deleting it")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "plan processing without full encode or replacement")
	flag.StringVar(&cfg.FFmpegPath, "ffmpeg", cfg.FFmpegPath, "path or executable name for ffmpeg")
	flag.StringVar(&cfg.FFprobePath, "ffprobe", cfg.FFprobePath, "path or executable name for ffprobe")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] DIRECTORY\n\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		return 2
	}
	cfg.Root = flag.Arg(0)
	cfg.FullDecodeCheck = !noFullDecode
	if err := cfg.Normalize(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	st, err := os.Stat(cfg.Root)
	if err != nil || !st.IsDir() {
		fmt.Fprintf(os.Stderr, "error: not a directory: %s\n", cfg.Root)
		return 2
	}
	if _, err := exec.LookPath(cfg.FFmpegPath); err != nil {
		fmt.Fprintf(os.Stderr, "error: ffmpeg not found: %v\n", err)
		return 1
	}
	if _, err := exec.LookPath(cfg.FFprobePath); err != nil {
		fmt.Fprintf(os.Stderr, "error: ffprobe not found: %v\n", err)
		return 1
	}

	logPath := filepath.Join(cfg.Root, "ffmpeg-compress.log")
	log, err := logging.New(logPath, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create log: %v\n", err)
		return 1
	}
	defer log.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	client := ffmpeg.New(cfg.FFmpegPath, cfg.FFprobePath)
	client.Logf = log.Printf
	if err := client.HasLibx265(ctx); err != nil {
		log.Printf("ERROR: %v", err)
		return 1
	}
	if requiresLibvmaf(cfg) {
		if err := client.HasLibvmaf(ctx); err != nil {
			log.Printf("ERROR: %v", err)
			return 1
		}
	}
	client.NVIDIA = client.DetectNVIDIA(ctx)
	log.Printf("NVIDIA-DETECT: nvenc=%t (%s) nvdec=%t (%s)",
		client.NVIDIA.NVENC, client.NVIDIA.NVENCReason, client.NVIDIA.NVDEC, client.NVIDIA.NVDECReason)
	selector, encoder, qualityMode, err := buildQualitySelector(cfg, client, log)
	if err != nil {
		log.Printf("ERROR: %v", err)
		return 1
	}
	decoder := "software"
	if client.NVIDIA.NVDEC {
		decoder = "nvdec-host-copy"
		if encoder == "hevc_nvenc" {
			decoder = "nvdec-zero-copy"
		}
	}
	proc := processor.Processor{Config: cfg, Media: client, Selector: selector, Logger: log}

	total, converted, skipped, failed := 0, 0, 0, 0
	var saved int64
	log.Printf("START root=%s quality_mode=%s encoder=%s decoder=%s preset=%s analysis_preset=%s vmaf_avg_min=%.4f vmaf_worst_min=%.4f ssim_avg_min=%.6f ssim_worst_min=%.6f sample_duration=%.3fs sample_count=%d min_savings=%.1f%% full_decode_check=%t keep_original=%t dry_run=%t",
		cfg.Root, qualityMode, encoder, decoder, cfg.Preset, cfg.AnalysisPreset, cfg.VMAFAverageMin, cfg.VMAFWorstMin, cfg.SSIMAverageMin, cfg.SSIMWorstMin, cfg.SampleDuration, cfg.SampleCount, cfg.MinSavings, cfg.FullDecodeCheck, cfg.KeepOriginal, cfg.DryRun)

	err = discovery.Walk(cfg.Root, func(path string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		total++
		r := proc.Process(ctx, path)
		switch r.Status {
		case processor.StatusConverted:
			converted++
			saved += r.SavedBytes
		case processor.StatusSkipped:
			skipped++
		case processor.StatusFailed:
			failed++
		}
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("Interrupted. Source file was not replaced unless its validation had already completed.")
			return 130
		}
		log.Printf("ERROR: recursive traversal failed: %v", err)
		failed++
	}
	log.Printf("DONE total=%d converted=%d skipped=%d failed=%d saved=%s", total, converted, skipped, failed, fsutil.HumanSize(saved))
	if failed > 0 {
		return 1
	}
	return 0
}

func requiresLibvmaf(cfg config.Config) bool {
	return cfg.DirectCRF == nil && cfg.DirectCQ == nil &&
		(cfg.QualityMetric == "vmaf" || cfg.QualityMetric == "both")
}

func setOptionalInt(target **int) func(string) error {
	return func(value string) error {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("must be an integer: %w", err)
		}
		*target = &parsed
		return nil
	}
}

func buildQualitySelector(cfg config.Config, client *ffmpeg.Client, log quality.Logger) (processor.Selector, string, string, error) {
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
		Preset: cfg.AnalysisPreset, Metric: quality.Metric(cfg.QualityMetric),
	}, encoder, cfg.QualityMetric + "-auto:20/18/16", nil
}
