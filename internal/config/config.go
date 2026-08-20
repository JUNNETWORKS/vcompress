package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Config struct {
	Root            string
	Preset          string
	AnalysisPreset  string
	DirectCRF       *int
	DirectCQ        *int
	QualityMetric   string
	VMAFAverageMin  float64
	VMAFWorstMin    float64
	SSIMAverageMin  float64
	SSIMWorstMin    float64
	SampleDuration  float64
	SampleCount     int
	MinSavings      float64
	FullDecodeCheck bool
	KeepOriginal    bool
	DryRun          bool
	FFmpegPath      string
	FFprobePath     string
}

func Default() Config {
	return Config{
		Preset:          "slow",
		QualityMetric:   "vmaf",
		VMAFAverageMin:  95.0,
		VMAFWorstMin:    90.0,
		SSIMAverageMin:  0.995,
		SSIMWorstMin:    0.992,
		SampleDuration:  4.0,
		SampleCount:     3,
		MinSavings:      5.0,
		FullDecodeCheck: true,
		KeepOriginal:    false,
		FFmpegPath:      "ffmpeg",
		FFprobePath:     "ffprobe",
	}
}

func (c *Config) Normalize() error {
	if c.Root == "" {
		return fmt.Errorf("directory is required")
	}
	abs, err := filepath.Abs(c.Root)
	if err != nil {
		return fmt.Errorf("resolve root directory: %w", err)
	}
	c.Root = filepath.Clean(abs)
	c.QualityMetric = strings.ToLower(strings.TrimSpace(c.QualityMetric))
	if c.AnalysisPreset == "" {
		c.AnalysisPreset = c.Preset
	}
	return c.Validate()
}

func (c Config) Validate() error {
	if c.Preset == "" {
		return fmt.Errorf("preset must not be empty")
	}
	if c.AnalysisPreset == "" {
		return fmt.Errorf("analysis preset must not be empty")
	}
	if c.DirectCRF != nil && c.DirectCQ != nil {
		return fmt.Errorf("crf and cq cannot be used together")
	}
	if c.DirectCRF != nil && (*c.DirectCRF < 0 || *c.DirectCRF > 51) {
		return fmt.Errorf("crf must be between 0 and 51")
	}
	if c.DirectCQ != nil && (*c.DirectCQ < 0 || *c.DirectCQ > 51) {
		return fmt.Errorf("cq must be between 0 and 51")
	}
	if c.QualityMetric != "vmaf" && c.QualityMetric != "ssim" && c.QualityMetric != "both" {
		return fmt.Errorf("quality-metric must be one of: vmaf, ssim, both")
	}
	if c.VMAFAverageMin < 0 || c.VMAFAverageMin > 100 {
		return fmt.Errorf("vmaf-average must satisfy 0 <= value <= 100")
	}
	if c.VMAFWorstMin < 0 || c.VMAFWorstMin > c.VMAFAverageMin {
		return fmt.Errorf("vmaf-worst must satisfy 0 <= value <= vmaf-average")
	}
	if c.SSIMAverageMin <= 0 || c.SSIMAverageMin > 1 {
		return fmt.Errorf("ssim-average must satisfy 0 < value <= 1")
	}
	if c.SSIMWorstMin <= 0 || c.SSIMWorstMin > c.SSIMAverageMin {
		return fmt.Errorf("ssim-worst must satisfy 0 < value <= ssim-average")
	}
	if c.SampleDuration <= 0 {
		return fmt.Errorf("sample-duration must be > 0")
	}
	if c.SampleCount < 1 || c.SampleCount > 5 {
		return fmt.Errorf("sample-count must be between 1 and 5")
	}
	if c.MinSavings < 0 || c.MinSavings > 100 {
		return fmt.Errorf("min-savings must be between 0 and 100")
	}
	if c.FFmpegPath == "" || c.FFprobePath == "" {
		return fmt.Errorf("ffmpeg and ffprobe paths must not be empty")
	}
	return nil
}
