package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
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

	flag.StringVar(&cfg.Preset, "preset", cfg.Preset, "final x265 preset")
	flag.StringVar(&cfg.AnalysisPreset, "analysis-preset", "", "x265 preset for sample analysis (default: same as -preset)")
	flag.Float64Var(&cfg.SSIMAverageMin, "ssim-average", cfg.SSIMAverageMin, "minimum average x265 SSIM Mean Y")
	flag.Float64Var(&cfg.SSIMWorstMin, "ssim-worst", cfg.SSIMWorstMin, "minimum worst-sample x265 SSIM Mean Y")
	flag.Float64Var(&cfg.SampleDuration, "sample-duration", cfg.SampleDuration, "seconds per representative sample")
	flag.IntVar(&cfg.SampleCount, "sample-count", cfg.SampleCount, "number of representative samples (1-5)")
	flag.Float64Var(&cfg.MinSavings, "min-savings", cfg.MinSavings, "minimum size reduction percent required for replacement")
	flag.BoolVar(&noFullDecode, "no-full-decode-check", false, "skip full decode verification of generated output")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "analyze and select CRF without full encode or replacement")
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
	if err := client.HasLibx265(ctx); err != nil {
		log.Printf("ERROR: %v", err)
		return 1
	}
	selector := quality.Selector{
		Measurer: client, Logger: log,
		AverageMin: cfg.SSIMAverageMin, WorstMin: cfg.SSIMWorstMin,
		SampleDuration: cfg.SampleDuration, SampleCount: cfg.SampleCount,
		Preset: cfg.AnalysisPreset,
	}
	proc := processor.Processor{Config: cfg, Media: client, Selector: selector, Logger: log}

	total, converted, skipped, failed := 0, 0, 0, 0
	var saved int64
	log.Printf("START root=%s auto_crf=20/18/16 preset=%s analysis_preset=%s ssim_avg_min=%.6f ssim_worst_min=%.6f sample_duration=%.3fs sample_count=%d min_savings=%.1f%% full_decode_check=%t dry_run=%t",
		cfg.Root, cfg.Preset, cfg.AnalysisPreset, cfg.SSIMAverageMin, cfg.SSIMWorstMin, cfg.SampleDuration, cfg.SampleCount, cfg.MinSavings, cfg.FullDecodeCheck, cfg.DryRun)

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
