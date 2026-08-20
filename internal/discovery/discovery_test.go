package discovery

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestIsVideoPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"movie.mp4", true},
		{"MOVIE.MKV", true},
		{"movie.txt", false},
		{".movie.ffmpeg-compressing.123.mp4", false},
		{".movie.crf-analysis.20.mkv", false},
	}
	for _, tt := range tests {
		if got := IsVideoPath(tt.path); got != tt.want {
			t.Errorf("IsVideoPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestWalkRecurses(t *testing.T) {
	root := t.TempDir()
	paths := []string{
		filepath.Join(root, "a.mp4"),
		filepath.Join(root, "nested", "b.MKV"),
		filepath.Join(root, "nested", "ignore.txt"),
	}
	if err := os.MkdirAll(filepath.Dir(paths[1]), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	if err := Walk(root, func(path string) error {
		got = append(got, filepath.Base(path))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	want := []string{"a.mp4", "b.MKV"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Walk() = %v, want %v", got, want)
	}
}

func TestListReturnsVideoCandidates(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a.mp4", "b.txt", "nested/c.MOV"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i] = filepath.Base(got[i])
	}
	sort.Strings(got)
	want := []string{"a.mp4", "c.MOV"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
}
