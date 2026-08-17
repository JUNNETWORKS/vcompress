package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type OutputPaths struct {
	Final string
	Temp  string
}

func PlanOutput(input string) (OutputPaths, error) {
	dir := filepath.Dir(input)
	ext := filepath.Ext(input)
	stem := strings.TrimSuffix(filepath.Base(input), ext)
	lowerExt := strings.ToLower(ext)

	final := input
	outExt := ext
	switch lowerExt {
	case ".mp4", ".m4v", ".mov", ".mkv":
	default:
		outExt = ".mkv"
		final = filepath.Join(dir, stem+outExt)
	}

	f, err := os.CreateTemp(dir, "."+stem+".ffmpeg-compressing-*"+outExt)
	if err != nil {
		return OutputPaths{}, fmt.Errorf("create temp output name: %w", err)
	}
	temp := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(temp)
		return OutputPaths{}, fmt.Errorf("close temp output placeholder: %w", err)
	}
	if err := os.Remove(temp); err != nil {
		return OutputPaths{}, fmt.Errorf("remove temp output placeholder: %w", err)
	}
	return OutputPaths{Final: final, Temp: temp}, nil
}

func SavingsPercent(oldSize, newSize int64) float64 {
	if oldSize <= 0 {
		return 0
	}
	return float64(oldSize-newSize) * 100 / float64(oldSize)
}

func HumanSize(bytes int64) string {
	const (
		kiB = int64(1024)
		miB = 1024 * kiB
		giB = 1024 * miB
	)
	switch {
	case bytes >= giB:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/float64(giB))
	case bytes >= miB:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/float64(miB))
	case bytes >= kiB:
		return fmt.Sprintf("%.2f KiB", float64(bytes)/float64(kiB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
