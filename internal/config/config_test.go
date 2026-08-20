package config

import "testing"

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
}

func TestValidateRejectsInvalidThresholds(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Config)
	}{
		{"average zero", func(c *Config) { c.SSIMAverageMin = 0 }},
		{"worst above average", func(c *Config) { c.SSIMWorstMin = 0.999; c.SSIMAverageMin = 0.995 }},
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
