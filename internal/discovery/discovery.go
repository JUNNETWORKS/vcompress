package discovery

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var extensions = map[string]struct{}{
	".mp4": {}, ".m4v": {}, ".mov": {}, ".mkv": {}, ".avi": {}, ".webm": {},
	".wmv": {}, ".flv": {}, ".mpg": {}, ".mpeg": {}, ".m2v": {}, ".ts": {},
	".mts": {}, ".m2ts": {}, ".vob": {}, ".3gp": {}, ".ogv": {},
}

func IsVideoPath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if strings.Contains(name, ".ffmpeg-compressing.") || strings.Contains(name, ".crf-analysis.") {
		return false
	}
	_, ok := extensions[strings.ToLower(filepath.Ext(path))]
	return ok
}

func Walk(root string, fn func(string) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !IsVideoPath(path) {
			return nil
		}
		return fn(path)
	})
}

func List(root string) ([]string, error) {
	return ListTargets([]string{root})
}

func ListTargets(targets []string) ([]string, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one target is required")
	}
	var paths []string
	seen := make(map[string]struct{})
	add := func(path string) error {
		abs, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolve candidate %q: %w", path, err)
		}
		abs = filepath.Clean(abs)
		if _, ok := seen[abs]; ok {
			return nil
		}
		seen[abs] = struct{}{}
		paths = append(paths, abs)
		return nil
	}
	for _, target := range targets {
		abs, err := filepath.Abs(target)
		if err != nil {
			return nil, fmt.Errorf("resolve target %q: %w", target, err)
		}
		abs = filepath.Clean(abs)
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("inspect target %q: %w", abs, err)
		}
		if info.IsDir() {
			if err := Walk(abs, add); err != nil {
				return nil, fmt.Errorf("walk target %q: %w", abs, err)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("target is not a regular file or directory: %s", abs)
		}
		if !IsVideoPath(abs) {
			return nil, fmt.Errorf("unsupported video file: %s", abs)
		}
		if err := add(abs); err != nil {
			return nil, err
		}
	}
	return paths, nil
}
