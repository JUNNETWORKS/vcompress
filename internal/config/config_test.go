package config

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDefaultIsValidAfterRoot(t *testing.T) {
	c := Default()
	c.Root = "."
	if err := c.Normalize(); err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if c.AnalysisPreset != c.Preset {
		t.Fatalf("analysis preset = %q, want %q", c.AnalysisPreset, c.Preset)
	}
	if c.KeepOriginal {
		t.Fatal("KeepOriginal = true, want default false")
	}
	if c.DirectCRF != nil || c.DirectCQ != nil {
		t.Fatalf("direct quality defaults = %v/%v, want nil/nil", c.DirectCRF, c.DirectCQ)
	}
	if c.QualityMetric != "vmaf" || c.VMAFAverageMin != 95 || c.VMAFWorstMin != 90 {
		t.Fatalf("VMAF defaults = %q/%v/%v, want vmaf/95/90", c.QualityMetric, c.VMAFAverageMin, c.VMAFWorstMin)
	}
}

func TestValidateRejectsInvalidThresholds(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
	}{
		{"average zero", func(c *Config) { c.SSIMAverageMin = 0 }},
		{"worst above average", func(c *Config) { c.SSIMWorstMin = 0.999; c.SSIMAverageMin = 0.995 }},
		{"unknown metric", func(c *Config) { c.QualityMetric = "psnr" }},
		{"VMAF average above 100", func(c *Config) { c.VMAFAverageMin = 101 }},
		{"VMAF worst above average", func(c *Config) { c.VMAFWorstMin = 96; c.VMAFAverageMin = 95 }},
		{"sample count zero", func(c *Config) { c.SampleCount = 0 }},
		{"sample count six", func(c *Config) { c.SampleCount = 6 }},
		{"negative savings", func(c *Config) { c.MinSavings = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			c.Root = "."
			c.AnalysisPreset = c.Preset
			tt.mut(&c)
			if err := c.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}

func TestNormalizeCanonicalizesQualityMetric(t *testing.T) {
	c := Default()
	c.Root = "."
	c.QualityMetric = " BOTH "
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	if c.QualityMetric != "both" {
		t.Fatalf("QualityMetric = %q, want both", c.QualityMetric)
	}
}

func TestNormalizeAcceptsAndDeduplicatesTargets(t *testing.T) {
	root := t.TempDir()
	c := Default()
	c.Targets = []string{root, filepath.Join(root, "."), filepath.Join(root, "movie.mp4")}
	if err := c.Normalize(); err != nil {
		t.Fatal(err)
	}
	want := []string{root, filepath.Join(root, "movie.mp4")}
	if !reflect.DeepEqual(c.TargetPaths(), want) {
		t.Fatalf("TargetPaths() = %v, want %v", c.TargetPaths(), want)
	}
	got := c.TargetPaths()
	got[0] = "changed"
	if c.Targets[0] == "changed" {
		t.Fatal("TargetPaths() returned the Config backing slice")
	}
}

func TestNormalizeRequiresRootOrTargets(t *testing.T) {
	c := Default()
	if err := c.Normalize(); err == nil {
		t.Fatal("Normalize() = nil, want missing target error")
	}
}

func TestValidateDirectQuality(t *testing.T) {
	value := func(v int) *int { return &v }
	tests := []struct {
		name string
		crf  *int
		cq   *int
		ok   bool
	}{
		{name: "unset", ok: true},
		{name: "crf zero", crf: value(0), ok: true},
		{name: "crf 51", crf: value(51), ok: true},
		{name: "cq zero", cq: value(0), ok: true},
		{name: "cq 51", cq: value(51), ok: true},
		{name: "both", crf: value(20), cq: value(20)},
		{name: "crf negative", crf: value(-1)},
		{name: "crf too high", crf: value(52)},
		{name: "cq negative", cq: value(-1)},
		{name: "cq too high", cq: value(52)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			c.Root = "."
			c.AnalysisPreset = c.Preset
			c.DirectCRF = tt.crf
			c.DirectCQ = tt.cq
			err := c.Validate()
			if (err == nil) != tt.ok {
				t.Fatalf("Validate() error = %v, want ok=%t", err, tt.ok)
			}
		})
	}
}
