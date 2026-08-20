package ffmpeg

import (
	"context"
	"errors"
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
	client.NVIDIA.NVDEC = true
	client.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "-x265-params ssim=1:log-level=info") {
			t.Fatalf("ffmpeg args do not enable x265 info logging: %s", joined)
		}
		if !strings.Contains(joined, "-hwaccel cuda") || strings.Contains(joined, "-hwaccel_output_format cuda") {
			t.Fatalf("x265 should download NVDEC frames to host memory: %s", joined)
		}
		return nil, []byte("encoded 100 frames, SSIM Mean Y: 0.9971234 (25.4 dB)"), nil
	})

	got, err := client.MeasureSSIM(context.Background(), "source.mp4", 0, "yuv420p", "slow", "libx265", 1, 4, 20)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.9971234 {
		t.Fatalf("MeasureSSIM() = %v", got)
	}
}

func TestParseSSIMFromFilterOutput(t *testing.T) {
	input := []byte(`[Parsed_ssim_2 @ 0x1] SSIM Y:0.996432 (24.5) U:0.998 V:0.998 All:0.997`)
	got, err := ParseSSIM(input)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.996432 {
		t.Fatalf("ParseSSIM() = %v", got)
	}
}

func TestDetectNVIDIARunsRuntimeProbes(t *testing.T) {
	client := New("ffmpeg", "ffprobe")
	client.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "-encoders"):
			return []byte("V....D libx265\nV....D hevc_nvenc"), nil, nil
		case strings.Contains(joined, "-hwaccels"):
			return []byte("Hardware acceleration methods:\ncuda"), nil, nil
		case strings.Contains(joined, "-c:v hevc_nvenc"):
			if !strings.Contains(joined, "color=c=black:s=1920x1080:r=1") {
				t.Fatalf("NVENC runtime probe uses an unsupported small frame: %s", joined)
			}
			return nil, nil, nil
		case strings.Contains(joined, "-c:v libx265"):
			return nil, nil, nil
		case strings.Contains(joined, "-hwaccel cuda"):
			if !strings.Contains(joined, "-hwaccel_output_format cuda") {
				t.Fatalf("NVDEC runtime probe does not retain CUDA frames: %s", joined)
			}
			return nil, nil, nil
		default:
			t.Fatalf("unexpected ffmpeg args: %s", joined)
			return nil, nil, nil
		}
	})

	got := client.DetectNVIDIA(context.Background())
	if !got.NVENC || !got.NVDEC {
		t.Fatalf("DetectNVIDIA() = %+v, want both available", got)
	}
}

func TestDetectNVIDIARejectsAdvertisedButUnusableNVENC(t *testing.T) {
	client := New("ffmpeg", "ffprobe")
	client.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "-encoders") {
			return []byte("V....D hevc_nvenc"), nil, nil
		}
		if strings.Contains(joined, "-c:v hevc_nvenc") {
			return nil, []byte("Cannot load libcuda.so.1"), errors.New("exit status 1")
		}
		if strings.Contains(joined, "-hwaccels") {
			return []byte("Hardware acceleration methods:"), nil, nil
		}
		t.Fatalf("unexpected ffmpeg args: %s", joined)
		return nil, nil, nil
	})

	got := client.DetectNVIDIA(context.Background())
	if got.NVENC || !strings.Contains(got.NVENCReason, "Cannot load libcuda") {
		t.Fatalf("DetectNVIDIA() = %+v", got)
	}
}

func TestDetectNVIDIARejectsAdvertisedButUnusableNVDEC(t *testing.T) {
	client := New("ffmpeg", "ffprobe")
	client.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.Contains(joined, "-encoders"):
			return []byte("V....D libx265"), nil, nil
		case strings.Contains(joined, "-hwaccels"):
			return []byte("Hardware acceleration methods:\ncuda"), nil, nil
		case strings.Contains(joined, "-c:v libx265"):
			return nil, nil, nil
		case strings.Contains(joined, "-hwaccel cuda"):
			return nil, []byte("No device available for decoder"), errors.New("exit status 1")
		default:
			t.Fatalf("unexpected ffmpeg args: %s", joined)
			return nil, nil, nil
		}
	})

	got := client.DetectNVIDIA(context.Background())
	if got.NVDEC || !strings.Contains(got.NVDECReason, "No device available") {
		t.Fatalf("DetectNVIDIA() = %+v", got)
	}
}

func TestMeasureSSIMWithNVENCUsesEncodedSample(t *testing.T) {
	client := New("ffmpeg", "ffprobe")
	client.NVIDIA.NVDEC = true
	var calls []string
	client.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.Contains(joined, "-filter_complex") {
			return nil, []byte("SSIM Y:0.998765 U:0.999 V:0.999 All:0.999"), nil
		}
		return nil, nil, nil
	})

	got, err := client.MeasureSSIM(context.Background(), "source.mp4", 1, "yuv420p", "slow", "hevc_nvenc", 2, 4, 18)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0.998765 {
		t.Fatalf("MeasureSSIM() = %v", got)
	}
	if len(calls) != 2 || !strings.Contains(calls[0], "-c:v hevc_nvenc") ||
		!strings.Contains(calls[0], "-preset p6 -tune hq -rc vbr -cq 18 -b:v 0") ||
		!strings.Contains(calls[1], "[0:v:1]setpts=PTS-STARTPTS") {
		t.Fatalf("unexpected calls: %v", calls)
	}
	if !strings.Contains(calls[0], "-hwaccel cuda -hwaccel_output_format cuda") ||
		strings.Contains(calls[0], "-pix_fmt yuv420p") {
		t.Fatalf("NVENC sample is not zero-copy: %s", calls[0])
	}
	if !strings.Contains(calls[0], "-fps_mode passthrough") {
		t.Fatalf("NVENC sample does not preserve frame timing: %s", calls[0])
	}
	if !strings.Contains(calls[1], "-hwaccel cuda") || strings.Contains(calls[1], "-hwaccel_output_format cuda") {
		t.Fatalf("SSIM comparison should download frames to host memory: %s", calls[1])
	}
}

func TestEncodeRetriesWithoutNVDEC(t *testing.T) {
	client := New("ffmpeg", "ffprobe")
	client.NVIDIA.NVDEC = true
	var calls []string
	client.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.Contains(joined, "-hwaccel cuda") {
			return nil, []byte("unsupported decoder"), errors.New("exit status 1")
		}
		return nil, nil, nil
	})

	err := client.Encode(context.Background(), EncodeOptions{
		Input: "source.mp4", Output: "output.mp4", PixFmt: "yuv420p", Preset: "slow", Quality: 20, Encoder: "hevc_nvenc",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !strings.Contains(calls[0], "-hwaccel cuda") || strings.Contains(calls[1], "-hwaccel cuda") {
		t.Fatalf("calls = %v, want hardware attempt then software decode", calls)
	}
	if !strings.Contains(calls[0], "-hwaccel_output_format cuda") || strings.Contains(calls[0], "-pix_fmt:v:0 yuv420p") {
		t.Fatalf("hardware attempt is not zero-copy: %s", calls[0])
	}
	if !strings.Contains(calls[1], "-c:v:0 hevc_nvenc") || !strings.Contains(calls[1], "-cq:v:0 20") {
		t.Fatalf("fallback changed encoder options: %s", calls[1])
	}
	if !strings.Contains(calls[1], "-pix_fmt:v:0 yuv420p") {
		t.Fatalf("software fallback did not restore pixel format: %s", calls[1])
	}
	for _, call := range calls {
		if !strings.Contains(call, "-fps_mode:v:0 passthrough") {
			t.Fatalf("final encode does not preserve frame timing: %s", call)
		}
	}
}

func TestFullDecodeKeepsFramesOnGPUAndFallsBack(t *testing.T) {
	client := New("ffmpeg", "ffprobe")
	client.NVIDIA.NVDEC = true
	var calls []string
	client.Runner = runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
		joined := strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.Contains(joined, "-hwaccel cuda") {
			return nil, []byte("hardware decode failed"), errors.New("exit status 1")
		}
		return nil, nil, nil
	})

	if err := client.FullDecode(context.Background(), "output.mp4", 1); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || !strings.Contains(calls[0], "-hwaccel cuda -hwaccel_output_format cuda") ||
		strings.Contains(calls[1], "-hwaccel") {
		t.Fatalf("calls = %v, want zero-copy validation then software decode", calls)
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
