package job

import (
	"context"
	"strings"
	"testing"

	"vcompress/internal/config"
)

func TestRunRecordsFinishTimeOnSetupFailure(t *testing.T) {
	cfg := config.Default()
	cfg.Root = t.TempDir()
	cfg.FFmpegPath = "definitely-not-vcompress-ffmpeg"
	summary, err := Run(context.Background(), cfg, Options{})
	if err == nil || !strings.Contains(err.Error(), "ffmpeg not found") {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.StartedAt.IsZero() || summary.FinishedAt.IsZero() || summary.FinishedAt.Before(summary.StartedAt) {
		t.Fatalf("summary times = %v -> %v", summary.StartedAt, summary.FinishedAt)
	}
}

func TestRequiresLibvmafHonorsDirectModes(t *testing.T) {
	cfg := config.Default()
	if !RequiresLibvmaf(cfg) {
		t.Fatal("automatic VMAF mode does not require libvmaf")
	}
	value := 20
	cfg.DirectCRF = &value
	if RequiresLibvmaf(cfg) {
		t.Fatal("direct CRF mode requires libvmaf")
	}
}
