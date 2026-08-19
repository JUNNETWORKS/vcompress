package fsutil

import (
	"path/filepath"
	"testing"
)

func TestPlanOutput(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name      string
		inputName string
		keep      bool
		finalName string
	}{
		{"mp4 replaced by default", "a.mp4", false, "a.mp4"},
		{"mkv replaced by default", "a.mkv", false, "a.mkv"},
		{"avi becomes mkv", "a.avi", false, "a.mkv"},
		{"mp4 retained beside hevc output", "a.mp4", true, "a.hevc.mp4"},
		{"uppercase mov retained beside hevc output", "a.MOV", true, "a.hevc.MOV"},
		{"avi retained beside mkv output", "a.avi", true, "a.mkv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := filepath.Join(dir, tt.inputName)
			p, err := PlanOutput(input, tt.keep)
			if err != nil {
				t.Fatal(err)
			}
			if p.Final != filepath.Join(dir, tt.finalName) {
				t.Fatalf("final = %q, want %q", p.Final, filepath.Join(dir, tt.finalName))
			}
			if filepath.Ext(p.Temp) != filepath.Ext(p.Final) {
				t.Fatalf("temp extension = %q, final extension = %q", filepath.Ext(p.Temp), filepath.Ext(p.Final))
			}
		})
	}
}

func TestSavingsPercent(t *testing.T) {
	if got := SavingsPercent(1000, 800); got != 20 {
		t.Fatalf("SavingsPercent() = %v, want 20", got)
	}
}
