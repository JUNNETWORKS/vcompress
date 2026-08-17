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

func (f *fakeMeasurer) MeasureSSIM(_ context.Context, _ string, _ int, _ string, _ string, _, _ float64, crf int) (float64, error) {
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
