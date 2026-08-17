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
