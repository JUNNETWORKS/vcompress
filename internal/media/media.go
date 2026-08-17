package media

import (
	"fmt"
	"strconv"
	"strings"
)

type VideoInfo struct {
	CodecName      string
	PixFmt         string
	Width          int
	Height         int
	AvgFrameRate   string
	ColorRange     string
	ColorSpace     string
	ColorTransfer  string
	ColorPrimaries string
	BitRate        int64
	Ordinal        int
	HDR            bool
}

type Info struct {
	Video        VideoInfo
	Duration     float64
	StreamCounts map[string]int
	ChapterCount int
}

func ParseFPS(v string) (float64, error) {
	v = strings.TrimSpace(v)
	if v == "" || v == "0/0" {
		return 0, nil
	}
	if strings.Contains(v, "/") {
		parts := strings.SplitN(v, "/", 2)
		if len(parts) != 2 {
			return 0, fmt.Errorf("invalid frame rate %q", v)
		}
		n, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, fmt.Errorf("parse frame-rate numerator: %w", err)
		}
		d, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0, fmt.Errorf("parse frame-rate denominator: %w", err)
		}
		if d == 0 {
			return 0, nil
		}
		return n / d, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("parse frame rate: %w", err)
	}
	return f, nil
}

func DurationClose(a, b float64) bool {
	if a <= 0 || b <= 0 {
		return true
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	tol := a * 0.01
	if tol < 1.0 {
		tol = 1.0
	}
	if tol > 3.0 {
		tol = 3.0
	}
	return d <= tol
}

func FPSClose(a, b string) bool {
	x, errX := ParseFPS(a)
	y, errY := ParseFPS(b)
	if errX != nil || errY != nil || x == 0 || y == 0 {
		return true
	}
	d := x - y
	if d < 0 {
		d = -d
	}
	return d <= 0.01
}

func SameOrUnknown(expected, actual string) bool {
	return expected == "" || expected == "unknown" || expected == actual
}
