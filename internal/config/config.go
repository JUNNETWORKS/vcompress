package config

import (
	"fmt"
	"path/filepath"
)

type Config struct {
	Root            string
	Preset          string
	AnalysisPreset  string
	SSIMAverageMin  float64
	SSIMWorstMin    float64
	SampleDuration  float64
	SampleCount     int
	MinSavings      float64
	FullDecodeCheck bool
	DryRun          bool
	FFmpegPath      string
	FFprobePath     string
}

func Default() Config {
	return Config{
		Preset:          "slow",
		SSIMAverageMin:  0.995,
		SSIMWorstMin:    0.992,
		SampleDuration:  4.0,
		SampleCount:     3,
		MinSavings:      5.0,
		FullDecodeCheck: true,
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
