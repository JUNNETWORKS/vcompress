package quality

import (
	"context"
	"fmt"
)

var Candidates = []int{20, 18, 16}

type Metric string

const (
	MetricVMAF Metric = "vmaf"
	MetricSSIM Metric = "ssim"
	MetricBoth Metric = "both"
)

type Scores struct {
	VMAF float64
	SSIM float64
}

type Measurer interface {
	MeasureQuality(ctx context.Context, input string, ordinal int, pixFmt, preset, encoder string, metric Metric, start, duration float64, value int) (Scores, error)
}

type Logger interface {
	Printf(format string, args ...any)
}

type Selector struct {
	Measurer         Measurer
	Logger           Logger
	SSIMAverageMin   float64
	SSIMWorstMin     float64
	SampleDuration   float64
	SampleCount      int
	Preset           string
	PreferredEncoder string
	FallbackEncoder  string
	Metric           Metric
	VMAFAverageMin   float64
	VMAFWorstMin     float64
}

type Result struct {
	Value       int
	VMAFAverage float64
	VMAFWorst   float64
	SSIMAverage float64
	SSIMWorst   float64
	Found       bool
	Encoder     string
	Metric      Metric
	Compared    bool
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
		vmafSum := 0.0
		vmafWorst := 100.0
		ssimSum := 0.0
		ssimWorst := 1.0
		for i, start := range starts {
			scores, err := s.Measurer.MeasureQuality(ctx, input, ordinal, pixFmt, s.Preset, encoder, s.Metric, start, effectiveDuration, crf)
			if err != nil {
				return Result{}, fmt.Errorf("encoder %s quality %d sample %d: %w", encoder, crf, i+1, err)
			}
			if s.Logger != nil {
				s.Logger.Printf("QUALITY-TEST: encoder=%s value=%d sample=%d start=%.3fs duration=%.3fs %s", encoder, crf, i+1, start, effectiveDuration, describeScores(s.Metric, scores))
			}
			if s.Metric == MetricVMAF || s.Metric == MetricBoth {
				vmafSum += scores.VMAF
				if scores.VMAF < vmafWorst {
					vmafWorst = scores.VMAF
				}
			}
			if s.Metric == MetricSSIM || s.Metric == MetricBoth {
				ssimSum += scores.SSIM
				if scores.SSIM < ssimWorst {
					ssimWorst = scores.SSIM
				}
			}
		}
		result := Result{Value: crf, Found: true, Encoder: encoder, Metric: s.Metric, Compared: true}
		if s.Metric == MetricVMAF || s.Metric == MetricBoth {
			result.VMAFAverage = vmafSum / float64(len(starts))
			result.VMAFWorst = vmafWorst
		}
		if s.Metric == MetricSSIM || s.Metric == MetricBoth {
			result.SSIMAverage = ssimSum / float64(len(starts))
			result.SSIMWorst = ssimWorst
		}
		if s.passes(result) {
			return result, nil
		}
		if s.Logger != nil {
			s.Logger.Printf("QUALITY-REJECT: encoder=%s value=%d %s", encoder, crf, s.describeAggregate(result))
		}
	}
	return Result{Found: false, Encoder: encoder, Metric: s.Metric, Compared: true}, nil
}

func (s Selector) passes(result Result) bool {
	vmafPass := result.VMAFAverage >= s.VMAFAverageMin && result.VMAFWorst >= s.VMAFWorstMin
	ssimPass := result.SSIMAverage >= s.SSIMAverageMin && result.SSIMWorst >= s.SSIMWorstMin
	switch s.Metric {
	case MetricVMAF:
		return vmafPass
	case MetricSSIM:
		return ssimPass
	case MetricBoth:
		return vmafPass && ssimPass
	default:
		return false
	}
}

func describeScores(metric Metric, scores Scores) string {
	switch metric {
	case MetricVMAF:
		return fmt.Sprintf("vmaf=%.4f", scores.VMAF)
	case MetricSSIM:
		return fmt.Sprintf("ssim=%.7f", scores.SSIM)
	default:
		return fmt.Sprintf("vmaf=%.4f ssim=%.7f", scores.VMAF, scores.SSIM)
	}
}

func (s Selector) describeAggregate(result Result) string {
	switch s.Metric {
	case MetricVMAF:
		return fmt.Sprintf("avg_vmaf=%.4f worst_vmaf=%.4f thresholds=%.4f/%.4f", result.VMAFAverage, result.VMAFWorst, s.VMAFAverageMin, s.VMAFWorstMin)
	case MetricSSIM:
		return fmt.Sprintf("avg_ssim=%.6f worst_ssim=%.6f thresholds=%.6f/%.6f", result.SSIMAverage, result.SSIMWorst, s.SSIMAverageMin, s.SSIMWorstMin)
	default:
		return fmt.Sprintf("avg_vmaf=%.4f worst_vmaf=%.4f vmaf_thresholds=%.4f/%.4f avg_ssim=%.6f worst_ssim=%.6f ssim_thresholds=%.6f/%.6f", result.VMAFAverage, result.VMAFWorst, s.VMAFAverageMin, s.VMAFWorstMin, result.SSIMAverage, result.SSIMWorst, s.SSIMAverageMin, s.SSIMWorstMin)
	}
}
