package ffmpeg

import (
	"context"
	"strings"
	"testing"
)

type runnerFunc func(ctx context.Context, name string, args ...string) ([]byte, []byte, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	return f(ctx, name, args...)
}

func TestParseSSIM(t *testing.T) {
	input := []byte(`x265 [info]: encoded 100 frames in 1.00s, 100.00 fps, 1000.00 kb/s, Avg QP:22.00, Global PSNR: 45.0, SSIM Mean Y: 0.9971234 (25.4 dB)`)
	got, err := ParseSSIM(input)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.9971234 {
		t.Fatalf("ParseSSIM() = %v", got)
	}
}

func TestMeasureSSIMEnablesX265InfoLog(t *testing.T) {
	client := New("ffmpeg", "ffprobe")
	client.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-x265-params ssim=1:log-level=info") {
			t.Fatalf("ffmpeg args do not enable x265 info logging: %s", joined)
		}
		return nil, []byte("encoded 100 frames, SSIM Mean Y: 0.9971234 (25.4 dB)"), nil
	})

	got, err := client.MeasureSSIM(context.Background(), "source.mp4", 0, "yuv420p", "slow", 1, 4, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.9971234 {
		t.Fatalf("MeasureSSIM() = %v", got)
	}
}

func TestParseProbeSelectsFirstNonAttachedVideo(t *testing.T) {
	input := []byte(`{
	  "streams": [
	    {"index":0,"codec_name":"mjpeg","codec_type":"video","pix_fmt":"yuvj420p","width":600,"height":600,"avg_frame_rate":"0/0","disposition":{"attached_pic":1}},
	    {"index":1,"codec_name":"h264","codec_type":"video","pix_fmt":"yuv420p","width":1920,"height":1080,"avg_frame_rate":"30000/1001","color_transfer":"bt709","bit_rate":"5000000","disposition":{"attached_pic":0}},
	    {"index":2,"codec_name":"aac","codec_type":"audio","disposition":{}}
	  ],
	  "chapters":[{}],
	  "format":{"duration":"60.5"}
	}`)
	got, err := ParseProbe(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Video.CodecName != "h264" || got.Video.Ordinal != 1 {
		t.Fatalf("video = %+v", got.Video)
	}
	if got.StreamCounts["video"] != 2 || got.StreamCounts["audio"] != 1 {
		t.Fatalf("stream counts = %#v", got.StreamCounts)
	}
	if got.ChapterCount != 1 || got.Duration != 60.5 {
		t.Fatalf("chapters/duration = %d/%v", got.ChapterCount, got.Duration)
	}
}

func TestParseProbeDetectsHDR(t *testing.T) {
	input := []byte(`{"streams":[{"index":0,"codec_name":"h264","codec_type":"video","pix_fmt":"yuv420p10le","width":3840,"height":2160,"avg_frame_rate":"24/1","color_transfer":"smpte2084","disposition":{"attached_pic":0}}],"format":{"duration":"10"}}`)
	got, err := ParseProbe(input)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Video.HDR {
		t.Fatal("HDR not detected")
	}
}
