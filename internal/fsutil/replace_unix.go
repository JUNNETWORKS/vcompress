//go:build !windows

package fsutil

import "os"

func ReplaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
