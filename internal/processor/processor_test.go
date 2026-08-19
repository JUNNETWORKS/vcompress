package processor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"vcompress/internal/config"
	ff "vcompress/internal/ffmpeg"
	"vcompress/internal/media"
	"vcompress/internal/quality"
)

type fakeMedia struct {
	probe       media.Info
	outputProbe media.Info
	encodeCalls int
	decodeCalls int
}

func (f *fakeMedia) Probe(_ context.Context, path string) (media.Info, error) {
	if f.encodeCalls > 0 && filepath.Base(path) != "source.mp4" {
		return f.outputProbe, nil
	}
	return f.probe, nil
}

func (f *fakeMedia) Encode(_ context.Context, o ff.EncodeOptions) error {
	f.encodeCalls++
	return os.WriteFile(o.Output, make([]byte, 500), 0o644)
}

func (f *fakeMedia) FullDecode(_ context.Context, _ string, _ int) error {
	f.decodeCalls++
	return nil
}

type fakeSelector struct{ result quality.Result }

func (f fakeSelector) Select(context.Context, string, int, string, float64) (quality.Result, error) {
	return f.result, nil
}

func baseInfo(codec string) media.Info {
	return media.Info{
		Video:    media.VideoInfo{CodecName: codec, PixFmt: "yuv420p", Width: 1920, Height: 1080, AvgFrameRate: "30/1", Ordinal: 0},
		Duration: 60, StreamCounts: map[string]int{"video": 1, "audio": 1}, ChapterCount: 0,
	}
}

func TestProcessSkipsEfficientCodec(t *testing.T) {
	m := &fakeMedia{probe: baseInfo("hevc")}
	p := Processor{Config: config.Default(), Media: m, Selector: fakeSelector{}}
	got := p.Process(context.Background(), filepath.Join(t.TempDir(), "source.mp4"))
	if got.Status != StatusSkipped || m.encodeCalls != 0 {
		t.Fatalf("result=%+v encodeCalls=%d", got, m.encodeCalls)
	}
}

func TestProcessDryRunDoesNotEncode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.mp4")
	if err := os.WriteFile(path, make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &fakeMedia{probe: baseInfo("h264")}
	c := config.Default()
	c.DryRun = true
	p := Processor{Config: c, Media: m, Selector: fakeSelector{result: quality.Result{Found: true, CRF: 20, Average: 0.999, Worst: 0.998}}}
	got := p.Process(context.Background(), path)
	if got.Status != StatusSkipped || m.encodeCalls != 0 {
		t.Fatalf("result=%+v encodeCalls=%d", got, m.encodeCalls)
	}
}

func TestProcessReplacesAfterValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.mp4")
	if err := os.WriteFile(path, make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	in := baseInfo("h264")
	out := baseInfo("hevc")
	m := &fakeMedia{probe: in, outputProbe: out}
	c := config.Default()
	c.MinSavings = 1
	p := Processor{Config: c, Media: m, Selector: fakeSelector{result: quality.Result{Found: true, CRF: 20, Average: 0.999, Worst: 0.998}}}
	got := p.Process(context.Background(), path)
	if got.Status != StatusConverted || got.SavedBytes != 500 {
		t.Fatalf("result=%+v", got)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 500 {
		t.Fatalf("source size = %d, want 500", st.Size())
	}
	if m.decodeCalls != 1 {
		t.Fatalf("decodeCalls = %d, want 1", m.decodeCalls)
	}
}

func TestProcessKeepOriginalPublishesBesideSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.mp4")
	original := bytes.Repeat([]byte{0x7f}, 1000)
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	in := baseInfo("h264")
	out := baseInfo("hevc")
	m := &fakeMedia{probe: in, outputProbe: out}
	c := config.Default()
	c.KeepOriginal = true
	c.MinSavings = 1
	p := Processor{Config: c, Media: m, Selector: fakeSelector{result: quality.Result{Found: true, CRF: 20, Average: 0.999, Worst: 0.998}}}

	got := p.Process(context.Background(), path)
	if got.Status != StatusConverted || got.SavedBytes != 0 {
		t.Fatalf("result=%+v, want converted with zero reclaimed bytes", got)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("source bytes changed with KeepOriginal enabled")
	}
	output := filepath.Join(dir, "source.hevc.mp4")
	st, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 500 {
		t.Fatalf("output size = %d, want 500", st.Size())
	}
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("output permissions = %o, want 640", st.Mode().Perm())
	}
	if m.decodeCalls != 1 {
		t.Fatalf("decodeCalls = %d, want 1", m.decodeCalls)
	}
}

func TestProcessKeepOriginalRefusesExistingOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.mp4")
	if err := os.WriteFile(path, make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "source.hevc.mp4")
	existing := []byte("existing output")
	if err := os.WriteFile(output, existing, 0o644); err != nil {
		t.Fatal(err)
	}
	m := &fakeMedia{probe: baseInfo("h264")}
	c := config.Default()
	c.KeepOriginal = true
	p := Processor{Config: c, Media: m, Selector: fakeSelector{result: quality.Result{Found: true, CRF: 20}}}

	got := p.Process(context.Background(), path)
	if got.Status != StatusSkipped || m.encodeCalls != 0 {
		t.Fatalf("result=%+v encodeCalls=%d", got, m.encodeCalls)
	}
	after, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, existing) {
		t.Fatal("existing output was modified")
	}
}

func TestProcessSkipsHDRWithoutEncoding(t *testing.T) {
	in := baseInfo("h264")
	in.Video.HDR = true
	m := &fakeMedia{probe: in}
	p := Processor{Config: config.Default(), Media: m, Selector: fakeSelector{result: quality.Result{Found: true, CRF: 20}}}
	got := p.Process(context.Background(), filepath.Join(t.TempDir(), "source.mp4"))
	if got.Status != StatusSkipped || m.encodeCalls != 0 {
		t.Fatalf("result=%+v encodeCalls=%d", got, m.encodeCalls)
	}
}

func TestProcessKeepsSourceWhenValidationFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.mp4")
	original := make([]byte, 1000)
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	in := baseInfo("h264")
	out := baseInfo("hevc")
	out.Video.Width = 1280 // Force validation failure.
	m := &fakeMedia{probe: in, outputProbe: out}
	c := config.Default()
	c.MinSavings = 0
	p := Processor{Config: c, Media: m, Selector: fakeSelector{result: quality.Result{Found: true, CRF: 20, Average: 0.999, Worst: 0.998}}}
	got := p.Process(context.Background(), path)
	if got.Status != StatusFailed {
		t.Fatalf("result=%+v, want failed", got)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != int64(len(original)) {
		t.Fatalf("source size = %d, want %d", st.Size(), len(original))
	}
}

func TestProcessKeepsSourceWhenSavingsBelowThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.mp4")
	if err := os.WriteFile(path, make([]byte, 1000), 0o644); err != nil {
		t.Fatal(err)
	}
	in := baseInfo("h264")
	out := baseInfo("hevc")
	m := &fakeMedia{probe: in, outputProbe: out}
	c := config.Default()
	c.MinSavings = 60 // fake encoder creates 500-byte output => 50% saving only.
	p := Processor{Config: c, Media: m, Selector: fakeSelector{result: quality.Result{Found: true, CRF: 20, Average: 0.999, Worst: 0.998}}}
	got := p.Process(context.Background(), path)
	if got.Status != StatusSkipped {
		t.Fatalf("result=%+v, want skipped", got)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != 1000 {
		t.Fatalf("source size = %d, want 1000", st.Size())
	}
}
