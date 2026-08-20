package quality

import (
	"context"
	"errors"
	"testing"

	"vcompress/internal/progress"
)

type fakeMeasurer struct {
	scores   map[int][]Scores
	calls    map[int]int
	errValue int
}

func (f *fakeMeasurer) MeasureQuality(_ context.Context, _ string, _ int, _ string, _, _ string, _ Metric, _, _ float64, value int) (Scores, error) {
	if value == f.errValue {
		return Scores{}, errors.New("boom")
	}
	if f.calls == nil {
		f.calls = map[int]int{}
	}
	i := f.calls[value]
	f.calls[value]++
	return f.scores[value][i], nil
}

type encoderMeasurer struct {
	failEncoder string
	calls       []string
}

func (m *encoderMeasurer) MeasureQuality(_ context.Context, _ string, _ int, _, _, encoder string, _ Metric, _, _ float64, _ int) (Scores, error) {
	m.calls = append(m.calls, encoder)
	if encoder == m.failEncoder {
		return Scores{}, errors.New("unsupported")
	}
	return Scores{VMAF: 99, SSIM: 0.999}, nil
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

func TestSelectUsesHighestPassingValueWithVMAF(t *testing.T) {
	m := &fakeMeasurer{scores: map[int][]Scores{
		20: {{VMAF: 94}, {VMAF: 93}, {VMAF: 94}},
		18: {{VMAF: 97}, {VMAF: 96}, {VMAF: 97}},
		16: {{VMAF: 99}, {VMAF: 99}, {VMAF: 99}},
	}}
	s := Selector{Measurer: m, Metric: MetricVMAF, VMAFAverageMin: 95, VMAFWorstMin: 90, SampleDuration: 4, SampleCount: 3, Preset: "slow"}
	got, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Value != 18 || !got.Compared || got.Metric != MetricVMAF {
		t.Fatalf("Select() = %+v, want value 18", got)
	}
	if m.calls[16] != 0 {
		t.Fatal("value 16 should not be tested after value 18 passes")
	}
}

func TestSelectBothRequiresBothMetrics(t *testing.T) {
	m := &fakeMeasurer{scores: map[int][]Scores{
		20: {{VMAF: 97, SSIM: 0.990}},
		18: {{VMAF: 96, SSIM: 0.996}},
	}}
	s := Selector{
		Measurer: m, Metric: MetricBoth,
		VMAFAverageMin: 95, VMAFWorstMin: 90,
		SSIMAverageMin: 0.995, SSIMWorstMin: 0.992,
		SampleDuration: 1, SampleCount: 1, Preset: "slow",
	}
	got, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Value != 18 || got.VMAFAverage != 96 || got.SSIMAverage != 0.996 {
		t.Fatalf("Select() = %+v, want both metrics to pass at 18", got)
	}
}

func TestSelectSSIMModeRemainsAvailable(t *testing.T) {
	m := &fakeMeasurer{scores: map[int][]Scores{20: {{SSIM: 0.996}}}}
	s := Selector{Measurer: m, Metric: MetricSSIM, SSIMAverageMin: 0.995, SSIMWorstMin: 0.992, SampleDuration: 1, SampleCount: 1, Preset: "slow"}
	got, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.SSIMAverage != 0.996 || got.Metric != MetricSSIM {
		t.Fatalf("Select() = %+v", got)
	}
}

func TestFixedSelectorReturnsWithoutComparison(t *testing.T) {
	s := FixedSelector{Value: 23, Encoder: "libx265"}
	got, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 100)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Found || got.Value != 23 || got.Encoder != "libx265" || got.Compared {
		t.Fatalf("Select() = %+v, want fixed unmeasured result", got)
	}
}

func TestSelectReturnsNotFound(t *testing.T) {
	m := &fakeMeasurer{scores: map[int][]Scores{
		20: {{VMAF: 80}}, 18: {{VMAF: 85}}, 16: {{VMAF: 89}},
	}}
	s := Selector{Measurer: m, Metric: MetricVMAF, VMAFAverageMin: 95, VMAFWorstMin: 90, SampleDuration: 1, SampleCount: 1, Preset: "slow"}
	got, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got.Found || got.Metric != MetricVMAF {
		t.Fatalf("Select() = %+v, want not found", got)
	}
}

func TestSelectPropagatesMeasurementError(t *testing.T) {
	m := &fakeMeasurer{scores: map[int][]Scores{20: {{VMAF: 90}}}, errValue: 20}
	s := Selector{Measurer: m, Metric: MetricVMAF, VMAFAverageMin: 95, VMAFWorstMin: 90, SampleDuration: 1, SampleCount: 1, Preset: "slow"}
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
		Measurer: m, Metric: MetricVMAF, VMAFAverageMin: 95, VMAFWorstMin: 90,
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

func TestSelectReportsEachMeasuredSample(t *testing.T) {
	m := &fakeMeasurer{scores: map[int][]Scores{20: {{VMAF: 99}, {VMAF: 98}}}}
	var events []progress.Event
	s := Selector{
		Measurer: m, Metric: MetricVMAF, VMAFAverageMin: 95, VMAFWorstMin: 90,
		SampleDuration: 1, SampleCount: 2, Preset: "slow",
		Reporter: progress.ReporterFunc(func(event progress.Event) { events = append(events, event) }),
	}
	if _, err := s.Select(context.Background(), "x.mp4", 0, "yuv420p", 10); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Sample != 1 || events[1].Sample != 2 || events[0].SampleCount != 2 {
		t.Fatalf("events = %+v", events)
	}
	for _, event := range events {
		if event.Phase != progress.PhaseQuality || event.QualityValue != 20 || event.Path != "x.mp4" {
			t.Fatalf("event = %+v", event)
		}
	}
}
