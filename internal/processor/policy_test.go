package processor

import "testing"

func TestPolicyForCodec(t *testing.T) {
	tests := []struct {
		codec string
		want  CodecPolicy
	}{
		{"h264", Transcode},
		{"mpeg4", Transcode},
		{"hevc", SkipEfficient},
		{"av1", SkipEfficient},
		{"prores", SkipArchivalOrUnknown},
		{"mystery", SkipArchivalOrUnknown},
	}
	for _, tt := range tests {
		if got := PolicyForCodec(tt.codec); got != tt.want {
			t.Errorf("PolicyForCodec(%q) = %v, want %v", tt.codec, got, tt.want)
		}
	}
}
