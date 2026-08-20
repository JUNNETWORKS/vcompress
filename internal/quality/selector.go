package quality

import (
	"context"
	"fmt"
)

var Candidates = []int{20, 18, 16}

type Measurer interface {
	MeasureSSIM(ctx context.Context, input string, ordinal int, pixFmt, preset, encoder string, start, duration float64, crf int) (float64, error)
}

type Logger interface {
	Printf(format string, args ...any)
}

type Selector struct {
	Measurer         Measurer
	Logger           Logger
	AverageMin       float64
	WorstMin         float64
	SampleDuration   float64
	SampleCount      int
	Preset           string
	PreferredEncoder string
	FallbackEncoder  string
}

type Result struct {
	Value        int
	Average      float64
	Worst        float64
	Found        bool
	Encoder      string
	SSIMCompared bool
}

type FixedSelector struct {
	Value   int
	Encoder string
}

func (s FixedSelector) Select(context.Context, string, int, string, float64) (Result, error) {
	return Result{Value: s.Value, Found: true, Encoder: s.Encoder}, nil
}

func SampleStarts(duration float64, count int, sampleDuration float64) []float64 {
	if duration <= 0 || count <= 0 || sampleDuration <= 0 {
		return nil
	}
	if sampleDuration > duration {
		sampleDuration = duration
	}
	maxStart := duration - sampleDuration
	starts := make([]float64, 0, count)
	for i := 1; i <= count; i++ {
		frac := float64(2*i-1) / float64(2*count)
		start := maxStart * frac
		if start < 0 {
			start = 0
		}
		starts = append(starts, start)
	}
	return starts
}

func (s Selector) Select(ctx context.Context, input string, ordinal int, pixFmt string, duration float64) (Result, error) {
	encoder := s.PreferredEncoder
	if encoder == "" {
		encoder = "libx265"
	}
	result, err := s.selectWithEncoder(ctx, input, ordinal, pixFmt, duration, encoder)
	if err == nil || s.FallbackEncoder == "" || s.FallbackEncoder == encoder || ctx.Err() != nil {
		return result, err
	}
	if s.Logger != nil {
		s.Logger.Printf("ENCODER-FALLBACK: encoder=%s failed during quality analysis; retrying all samples with encoder=%s | %v", encoder, s.FallbackEncoder, err)
	}
	return s.selectWithEncoder(ctx, input, ordinal, pixFmt, duration, s.FallbackEncoder)
}

func (s Selector) selectWithEncoder(ctx context.Context, input string, ordinal int, pixFmt string, duration float64, encoder string) (Result, error) {
	starts := SampleStarts(duration, s.SampleCount, s.SampleDuration)
	if len(starts) == 0 {
		return Result{}, fmt.Errorf("no valid sample positions")
	}
	effectiveDuration := s.SampleDuration
	if duration < effectiveDuration {
		effectiveDuration = duration
	}

	for _, crf := range Candidates {
		sum := 0.0
		worst := 1.0
		for i, start := range starts {
			score, err := s.Measurer.MeasureSSIM(ctx, input, ordinal, pixFmt, s.Preset, encoder, start, effectiveDuration, crf)
			if err != nil {
				return Result{}, fmt.Errorf("encoder %s quality %d sample %d: %w", encoder, crf, i+1, err)
			}
			if s.Logger != nil {
				s.Logger.Printf("QUALITY-TEST: encoder=%s value=%d sample=%d start=%.3fs duration=%.3fs ssim=%.7f", encoder, crf, i+1, start, effectiveDuration, score)
			}
			sum += score
			if score < worst {
				worst = score
			}
		}
		avg := sum / float64(len(starts))
		if avg >= s.AverageMin && worst >= s.WorstMin {
			return Result{Value: crf, Average: avg, Worst: worst, Found: true, Encoder: encoder, SSIMCompared: true}, nil
		}
		if s.Logger != nil {
			s.Logger.Printf("QUALITY-REJECT: encoder=%s value=%d avg_ssim=%.6f worst_ssim=%.6f thresholds=%.6f/%.6f", encoder, crf, avg, worst, s.AverageMin, s.WorstMin)
		}
	}
	return Result{Found: false, Encoder: encoder}, nil
}
