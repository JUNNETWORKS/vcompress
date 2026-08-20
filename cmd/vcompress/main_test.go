package main

import (
	"bytes"
	"context"
	"flag"
	"strings"
	"testing"

	"vcompress/internal/config"
	"vcompress/internal/ffmpeg"
)

func TestCLIUsageMentionsWebCommand(t *testing.T) {
	var output bytes.Buffer
	flags := flag.NewFlagSet("vcompress", flag.ContinueOnError)
	flags.SetOutput(&output)

	printCLIUsage(&output, "vcompress", flags)

	usage := output.String()
	for _, want := range []string{"vcompress web [options]", "web    start the local WebUI"} {
		if !strings.Contains(usage, want) {
			t.Fatalf("usage = %q, want %q", usage, want)
		}
	}
}

func TestCLIHelpReturnsSuccess(t *testing.T) {
	if got := runCLI([]string{"-help"}); got != 0 {
		t.Fatalf("runCLI(-help) = %d, want 0", got)
	}
}

func TestRequiresLibvmafOnlyForAutomaticVMAFModes(t *testing.T) {
	value := 20
	tests := []struct {
		name string
		cfg  config.Config
		want bool
	}{
		{name: "VMAF", cfg: config.Config{QualityMetric: "vmaf"}, want: true},
		{name: "both", cfg: config.Config{QualityMetric: "both"}, want: true},
		{name: "SSIM", cfg: config.Config{QualityMetric: "ssim"}},
		{name: "direct CRF", cfg: config.Config{QualityMetric: "vmaf", DirectCRF: &value}},
		{name: "direct CQ", cfg: config.Config{QualityMetric: "both", DirectCQ: &value}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := requiresLibvmaf(tt.cfg); got != tt.want {
				t.Fatalf("requiresLibvmaf() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestSetOptionalInt(t *testing.T) {
	var got *int
	if err := setOptionalInt(&got)("0"); err != nil {
		t.Fatal(err)
	}
	if got == nil || *got != 0 {
		t.Fatalf("parsed value = %v, want explicit zero", got)
	}
	if err := setOptionalInt(&got)("not-an-int"); err == nil {
		t.Fatal("setOptionalInt() error = nil, want parse error")
	}
}

func TestBuildQualitySelectorDirectCRFForcesLibx265(t *testing.T) {
	value := 24
	cfg := config.Default()
	cfg.DirectCRF = &value
	client := ffmpeg.New("ffmpeg", "ffprobe")
	client.NVIDIA.NVENC = true

	selector, encoder, mode, err := buildQualitySelector(cfg, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := selector.Select(context.Background(), "source.mp4", 0, "yuv420p", 60)
	if err != nil {
		t.Fatal(err)
	}
	if encoder != "libx265" || result.Encoder != "libx265" || result.Value != 24 || result.Compared {
		t.Fatalf("strategy = encoder=%s mode=%s result=%+v", encoder, mode, result)
	}
	if !strings.Contains(mode, "analysis-skipped") {
		t.Fatalf("mode = %q, want analysis skip marker", mode)
	}
}

func TestBuildQualitySelectorDirectCQRequiresNVENC(t *testing.T) {
	value := 21
	cfg := config.Default()
	cfg.DirectCQ = &value
	client := ffmpeg.New("ffmpeg", "ffprobe")
	client.NVIDIA.NVENCReason = "runtime probe failed"

	if _, _, _, err := buildQualitySelector(cfg, client, nil); err == nil || !strings.Contains(err.Error(), "runtime probe failed") {
		t.Fatalf("buildQualitySelector() error = %v, want NVENC reason", err)
	}

	client.NVIDIA.NVENC = true
	selector, encoder, mode, err := buildQualitySelector(cfg, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := selector.Select(context.Background(), "source.mp4", 0, "yuv420p", 60)
	if err != nil {
		t.Fatal(err)
	}
	if encoder != "hevc_nvenc" || result.Encoder != "hevc_nvenc" || result.Value != 21 || result.Compared {
		t.Fatalf("strategy = encoder=%s mode=%s result=%+v", encoder, mode, result)
	}
}

func TestBuildQualitySelectorAutomaticKeepsNVENCFallback(t *testing.T) {
	cfg := config.Default()
	cfg.AnalysisPreset = cfg.Preset
	client := ffmpeg.New("ffmpeg", "ffprobe")
	client.NVIDIA.NVENC = true

	_, encoder, mode, err := buildQualitySelector(cfg, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	if encoder != "hevc_nvenc" || mode != "vmaf-auto:20/18/16" {
		t.Fatalf("encoder/mode = %q/%q", encoder, mode)
	}
}
