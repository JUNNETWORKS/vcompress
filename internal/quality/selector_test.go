package quality

import (
	"context"
	"errors"
	"testing"
)

type fakeMeasurer struct {
	scores map[int][]float64
	calls  map[int]int
	errCRF int
}

func (f *fakeMeasurer) MeasureSSIM(_ context.Context, _ string, _ int, _ string, _, _ string, _, _ float64, crf int) (float64, error) {
	if crf == f.errCRF {
		return 0, errors.New("boom")
	}
	if f.calls == nil {
		f.calls = map[int]int{}
	}
	i := f.calls[crf]
	f.calls[crf]++
	return f.scores[crf][i], nil
}

type encoderMeasurer struct {
	failEncoder string
	calls       []string
}

func (m *encoderMeasurer) MeasureSSIM(_ context.Context, _ string, _ int, _, _, encoder string, _, _ float64, _ int) (float64, error) {
	m.calls = append(m.calls, encoder)
	if encoder == m.failEncoder {
		return 0, errors.New("unsupported")
	}
	return 0.999, nil
}

func TestSampleStarts(t *testing.T) {
	got := SampleStarts(100, 3, 4)
	want := []float64{16, 48, 80}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("starts[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestSelectUsesHighestPassingCRF(t *testing.T) {
	m := &fakeMeasurer{scores: map[int][]float64{
		20: {0.994, 0.993, 0.994},
		18: {0.997, 0.996, 0.997},
		16: {0.999, 0.999, 0.999},
	}}
	s := Selector{Measurer: m, AverageMin: 0.995, WorstMin: 0.992, SampleDuration: 4, SampleCount: 3, Preset: "slow"}
	got, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.CRF != 18 {
		t.Fatalf("Select() = %+v, want CRF 18", got)
	}
	if m.calls[16] != 0 {
		t.Fatal("CRF 16 should not be tested after CRF 18 passes")
	}
}

func TestSelectReturnsNotFound(t *testing.T) {
	m := &fakeMeasurer{scores: map[int][]float64{
		20: {0.9}, 18: {0.91}, 16: {0.92},
	}}
	s := Selector{Measurer: m, AverageMin: 0.995, WorstMin: 0.992, SampleDuration: 1, SampleCount: 1, Preset: "slow"}
	got, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.Found {
		t.Fatalf("Select() = %+v, want not found", got)
	}
}

func TestSelectPropagatesMeasurementError(t *testing.T) {
	m := &fakeMeasurer{scores: map[int][]float64{20: {0.9}}, errCRF: 20}
	s := Selector{Measurer: m, AverageMin: 0.995, WorstMin: 0.992, SampleDuration: 1, SampleCount: 1, Preset: "slow"}
	if _, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 10); err == nil {
		t.Fatal("Select() error = nil, want error")
	}
}

func TestSampleStartsShortVideoUsesZeroStart(t *testing.T) {
	got := SampleStarts(2, 3, 4)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, v := range got {
		if v != 0 {
			t.Fatalf("starts[%d] = %v, want 0", i, v)
		}
	}
}

func TestSelectRestartsWithFallbackEncoder(t *testing.T) {
	m := &encoderMeasurer{failEncoder: "hevc_nvenc"}
	s := Selector{
		Measurer: m, AverageMin: 0.995, WorstMin: 0.992,
		SampleDuration: 1, SampleCount: 2, Preset: "slow",
		PreferredEncoder: "hevc_nvenc", FallbackEncoder: "libx265",
	}
	got, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Encoder != "libx265" {
		t.Fatalf("Select() = %+v, want libx265 fallback", got)
	}
	want := []string{"hevc_nvenc", "libx265", "libx265"}
	if len(m.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", m.calls, want)
	}
	for i := range want {
		if m.calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", m.calls, want)
		}
	}
}
