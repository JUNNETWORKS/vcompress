//go:build integration

package processor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"vcompress/internal/config"
	"vcompress/internal/ffmpeg"
	"vcompress/internal/logging"
	"vcompress/internal/quality"
)

func TestIntegrationMPEG4ToHEVC(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	ctx := context.Background()
	client := ffmpeg.New("ffmpeg", "ffprobe")
	if err := client.HasLibx265(ctx); err != nil {
		t.Skip(err)
	}

	for _, tt := range []struct {
		name         string
		keepOriginal bool
		directCRF    bool
	}{
		{name: "replace source"},
		{name: "keep original", keepOriginal: true},
		{name: "direct CRF without SSIM", directCRF: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "source.mp4")
			cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
				"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30",
				"-f", "lavfi", "-i", "sine=frequency=1000:sample_rate=48000",
				"-t", "5", "-c:v", "mpeg4", "-q:v", "2", "-c:a", "aac", input)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("create fixture: %v\n%s", err, out)
			}

			cfg := config.Default()
			cfg.Root = dir
			cfg.Preset = "ultrafast"
			cfg.AnalysisPreset = "ultrafast"
			cfg.SampleCount = 1
			cfg.SampleDuration = 0.5
			cfg.SSIMAverageMin = 0.90
			cfg.SSIMWorstMin = 0.90
			cfg.MinSavings = 0
			cfg.KeepOriginal = tt.keepOriginal

			log := logging.NewWriter(os.Stdout)
			var sel Selector = quality.Selector{Measurer: client, Logger: log, AverageMin: cfg.SSIMAverageMin, WorstMin: cfg.SSIMWorstMin, SampleDuration: cfg.SampleDuration, SampleCount: cfg.SampleCount, Preset: cfg.AnalysisPreset}
			if tt.directCRF {
				sel = quality.FixedSelector{Value: 24, Encoder: "libx265"}
			}
			p := Processor{Config: cfg, Media: client, Selector: sel, Logger: log}
			result := p.Process(ctx, input)
			if result.Status != StatusConverted {
				t.Fatalf("Process() status = %v", result.Status)
			}

			output := input
			if tt.keepOriginal {
				output = filepath.Join(dir, "source.hevc.mp4")
				originalInfo, err := client.Probe(ctx, input)
				if err != nil {
					t.Fatal(err)
				}
				if originalInfo.Video.CodecName != "mpeg4" {
					t.Fatalf("source codec = %s, want mpeg4", originalInfo.Video.CodecName)
				}
			}
			info, err := client.Probe(ctx, output)
			if err != nil {
				t.Fatal(err)
			}
			if info.Video.CodecName != "hevc" {
				t.Fatalf("output codec = %s, want hevc", info.Video.CodecName)
			}
		})
	}
}
