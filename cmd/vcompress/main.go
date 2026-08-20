package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"vcompress/internal/config"
	"vcompress/internal/ffmpeg"
	"vcompress/internal/job"
	"vcompress/internal/processor"
	"vcompress/internal/quality"
	"vcompress/internal/webui"
)

func main() {
	os.Exit(run())
}

func run() int {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "web" {
		return runWeb(args[1:])
	}
	return runCLI(args)
}

func runWeb(args []string) int {
	opts, err := webui.ParseWebFlags(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}
	opts.Output = os.Stdout
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := webui.Serve(ctx, opts); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func runCLI(args []string) int {
	cfg := config.Default()
	noFullDecode := false
	flags := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.StringVar(&cfg.Preset, "preset", cfg.Preset, "final encoder preset (x265 name; mapped to NVENC p1-p7)")
	flags.StringVar(&cfg.AnalysisPreset, "analysis-preset", "", "sample encoder preset (default: same as -preset)")
	flags.Func("crf", "use libx265 CRF directly (0-51) and skip quality analysis", setOptionalInt(&cfg.DirectCRF))
	flags.Func("cq", "use NVENC CQ directly (0-51) and skip quality analysis", setOptionalInt(&cfg.DirectCQ))
	flags.StringVar(&cfg.QualityMetric, "quality-metric", cfg.QualityMetric, "automatic quality metric: vmaf, ssim, or both")
	flags.Float64Var(&cfg.VMAFAverageMin, "vmaf-average", cfg.VMAFAverageMin, "minimum average VMAF score")
	flags.Float64Var(&cfg.VMAFWorstMin, "vmaf-worst", cfg.VMAFWorstMin, "minimum worst-sample VMAF score")
	flags.Float64Var(&cfg.SSIMAverageMin, "ssim-average", cfg.SSIMAverageMin, "minimum average SSIM Mean Y")
	flags.Float64Var(&cfg.SSIMWorstMin, "ssim-worst", cfg.SSIMWorstMin, "minimum worst-sample SSIM Mean Y")
	flags.Float64Var(&cfg.SampleDuration, "sample-duration", cfg.SampleDuration, "seconds per representative sample")
	flags.IntVar(&cfg.SampleCount, "sample-count", cfg.SampleCount, "number of representative samples (1-5)")
	flags.Float64Var(&cfg.MinSavings, "min-savings", cfg.MinSavings, "minimum size reduction percent required for replacement")
	flags.BoolVar(&noFullDecode, "no-full-decode-check", false, "skip full decode verification of generated output")
	flags.BoolVar(&cfg.KeepOriginal, "keep-original", cfg.KeepOriginal, "publish compressed output beside the source without deleting it")
	flags.BoolVar(&cfg.DryRun, "dry-run", false, "plan processing without full encode or replacement")
	flags.StringVar(&cfg.FFmpegPath, "ffmpeg", cfg.FFmpegPath, "path or executable name for ffmpeg")
	flags.StringVar(&cfg.FFprobePath, "ffprobe", cfg.FFprobePath, "path or executable name for ffprobe")
	flags.Usage = func() {
		printCLIUsage(flags.Output(), filepath.Base(os.Args[0]), flags)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return 2
	}
	cfg.Root = flags.Arg(0)
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	summary, err := job.Run(ctx, cfg, job.Options{Console: os.Stdout})
	if err != nil {
		if job.IsInterrupted(err) {
			return 130
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if summary.Failed > 0 {
		return 1
	}
	return 0
}

func printCLIUsage(output io.Writer, program string, flags *flag.FlagSet) {
	fmt.Fprintf(output, "Usage:\n  %s [options] DIRECTORY\n  %s web [options]\n\n", program, program)
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  web    start the local WebUI")
	fmt.Fprintln(output, "\nOptions:")
	flags.PrintDefaults()
}

func requiresLibvmaf(cfg config.Config) bool {
	return job.RequiresLibvmaf(cfg)
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
	return job.BuildQualitySelector(cfg, client, log, nil)
}
