package discovery

import (
	"io/fs"
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
