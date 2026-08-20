package job

import (
	"context"
	"os"
	"path/filepath"
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

func TestRunAcceptsFileTarget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.mp4")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Targets = []string{path}
	cfg.FFmpegPath = "definitely-not-vcompress-ffmpeg"
	_, err := Run(context.Background(), cfg, Options{})
	if err == nil || !strings.Contains(err.Error(), "ffmpeg not found") {
		t.Fatalf("Run() error = %v, want ffmpeg lookup after target validation", err)
	}
}

func TestTargetLogDirectoryUsesDirectoryOrFileParent(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "movie.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{root, file} {
		got, err := targetLogDirectory(target)
		if err != nil {
			t.Fatal(err)
		}
		if got != root {
			t.Fatalf("targetLogDirectory(%q) = %q, want %q", target, got, root)
		}
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
