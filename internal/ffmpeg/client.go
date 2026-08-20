package ffmpeg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"vcompress/internal/media"
	"vcompress/internal/quality"
)

type Client struct {
	FFmpeg  string
	FFprobe string
	Runner  Runner
	NVIDIA  NVIDIACapabilities
	Logf    func(format string, args ...any)
}

type NVIDIACapabilities struct {
	NVENC       bool
	NVDEC       bool
	NVENCReason string
	NVDECReason string
}

func New(ffmpegPath, ffprobePath string) *Client {
	return &Client{FFmpeg: ffmpegPath, FFprobe: ffprobePath, Runner: OSRunner{}}
}

type probeJSON struct {
	Streams []struct {
		Index          int    `json:"index"`
		CodecName      string `json:"codec_name"`
		CodecType      string `json:"codec_type"`
		PixFmt         string `json:"pix_fmt"`
		Width          int    `json:"width"`
		Height         int    `json:"height"`
		AvgFrameRate   string `json:"avg_frame_rate"`
		ColorRange     string `json:"color_range"`
		ColorSpace     string `json:"color_space"`
		ColorTransfer  string `json:"color_transfer"`
		ColorPrimaries string `json:"color_primaries"`
		BitRate        string `json:"bit_rate"`
		Disposition    struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
		SideDataList []struct {
			SideDataType string `json:"side_data_type"`
		} `json:"side_data_list"`
	} `json:"streams"`
	Chapters []json.RawMessage `json:"chapters"`
	Format   struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func ParseProbe(data []byte) (media.Info, error) {
	var raw probeJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return media.Info{}, fmt.Errorf("decode ffprobe JSON: %w", err)
	}
	info := media.Info{StreamCounts: map[string]int{}, ChapterCount: len(raw.Chapters)}
	if raw.Format.Duration != "" {
		if d, err := strconv.ParseFloat(raw.Format.Duration, 64); err == nil {
			info.Duration = d
		}
	}

	videoOrdinal := 0
	foundMain := false
	for _, s := range raw.Streams {
		info.StreamCounts[s.CodecType]++
		if s.CodecType != "video" {
			continue
		}
		if !foundMain && s.Disposition.AttachedPic != 1 {
			bitRate, _ := strconv.ParseInt(s.BitRate, 10, 64)
			info.Video = media.VideoInfo{
				CodecName:      s.CodecName,
				PixFmt:         s.PixFmt,
				Width:          s.Width,
				Height:         s.Height,
				AvgFrameRate:   s.AvgFrameRate,
				ColorRange:     s.ColorRange,
				ColorSpace:     s.ColorSpace,
				ColorTransfer:  s.ColorTransfer,
				ColorPrimaries: s.ColorPrimaries,
				BitRate:        bitRate,
				Ordinal:        videoOrdinal,
			}
			info.Video.HDR = isHDR(info.Video.ColorTransfer, s.SideDataList)
			foundMain = true
		}
		videoOrdinal++
	}
	if !foundMain {
		return media.Info{}, errors.New("no non-attached video stream")
	}
	if info.Video.CodecName == "" || info.Video.PixFmt == "" || info.Video.Width <= 0 || info.Video.Height <= 0 {
		return media.Info{}, errors.New("main video stream is missing required fields")
	}
	return info, nil
}

func isHDR(transfer string, sideData []struct {
	SideDataType string `json:"side_data_type"`
}) bool {
	if transfer == "smpte2084" || transfer == "arib-std-b67" {
		return true
	}
	for _, sd := range sideData {
		s := strings.ToLower(sd.SideDataType)
		if strings.Contains(s, "dolby") || strings.Contains(s, "dovi") || strings.Contains(s, "hdr10+") ||
			strings.Contains(s, "dynamic metadata") || strings.Contains(s, "mastering display") ||
			strings.Contains(s, "content light") {
			return true
		}
	}
	return false
}

func (c *Client) Probe(ctx context.Context, path string) (media.Info, error) {
	stdout, stderr, err := c.Runner.Run(ctx, c.FFprobe,
		"-v", "error", "-show_streams", "-show_chapters", "-show_format", "-of", "json", path,
	)
	if err != nil {
		return media.Info{}, fmt.Errorf("ffprobe failed: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	return ParseProbe(stdout)
}

func (c *Client) HasLibx265(ctx context.Context) error {
	stdout, stderr, err := c.Runner.Run(ctx, c.FFmpeg, "-hide_banner", "-encoders")
	if err != nil {
		return fmt.Errorf("ffmpeg -encoders failed: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	if !strings.Contains(string(stdout), "libx265") && !strings.Contains(string(stderr), "libx265") {
		return errors.New("this FFmpeg build does not provide the libx265 encoder")
	}
	return nil
}

func (c *Client) HasLibvmaf(ctx context.Context) error {
	stdout, stderr, err := c.Runner.Run(ctx, c.FFmpeg, "-hide_banner", "-filters")
	if err != nil {
		return fmt.Errorf("ffmpeg -filters failed: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	for _, field := range strings.Fields(string(stdout) + "\n" + string(stderr)) {
		if field == "libvmaf" {
			return nil
		}
	}
	return errors.New("automatic VMAF analysis requires an FFmpeg build with the libvmaf filter")
}

func (c *Client) DetectNVIDIA(ctx context.Context) NVIDIACapabilities {
	caps := NVIDIACapabilities{
		NVENCReason: "hevc_nvenc is not present in this FFmpeg build",
		NVDECReason: "CUDA hardware acceleration is not present in this FFmpeg build",
	}

	stdout, stderr, err := c.Runner.Run(ctx, c.FFmpeg, "-hide_banner", "-encoders")
	if err != nil {
		caps.NVENCReason = commandFailure("ffmpeg -encoders", err, stderr)
	} else if strings.Contains(string(stdout)+string(stderr), "hevc_nvenc") {
		_, probeStderr, probeErr := c.Runner.Run(ctx, c.FFmpeg,
			"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=1920x1080:r=1",
			"-frames:v", "1", "-an", "-c:v", "hevc_nvenc", "-preset", "p1", "-f", "null", "-",
		)
		if probeErr == nil {
			caps.NVENC = true
			caps.NVENCReason = "runtime probe succeeded"
		} else {
			caps.NVENCReason = commandFailure("hevc_nvenc runtime probe", probeErr, probeStderr)
		}
	}

	stdout, stderr, err = c.Runner.Run(ctx, c.FFmpeg, "-hide_banner", "-hwaccels")
	if err != nil {
		caps.NVDECReason = commandFailure("ffmpeg -hwaccels", err, stderr)
	} else if strings.Contains(strings.ToLower(string(stdout)+string(stderr)), "cuda") {
		caps.NVDEC, caps.NVDECReason = c.probeNVDEC(ctx)
	}
	return caps
}

func (c *Client) probeNVDEC(ctx context.Context) (bool, string) {
	path, cleanup, err := temporaryMediaPath("vcompress-nvdec-probe-*.mkv")
	if err != nil {
		return false, err.Error()
	}
	defer cleanup()

	_, stderr, err := c.Runner.Run(ctx, c.FFmpeg,
		"-hide_banner", "-y", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=64x64:r=1",
		"-frames:v", "1", "-an", "-c:v", "libx265", "-preset", "ultrafast", path,
	)
	if err != nil {
		return false, commandFailure("create NVDEC probe stream", err, stderr)
	}
	_, stderr, err = c.Runner.Run(ctx, c.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-hwaccel", "cuda", "-hwaccel_output_format", "cuda", "-i", path,
		"-map", "0:v:0", "-frames:v", "1", "-f", "null", "-",
	)
	if err != nil {
		return false, commandFailure("NVDEC runtime probe", err, stderr)
	}
	return true, "runtime probe succeeded"
}

func commandFailure(operation string, err error, stderr []byte) string {
	detail := tail(string(stderr), 12)
	if detail == "" {
		return fmt.Sprintf("%s failed: %v", operation, err)
	}
	return fmt.Sprintf("%s failed: %v: %s", operation, err, detail)
}

var (
	filterSSIMRE = regexp.MustCompile(`SSIM Y:\s*([0-9]+(?:\.[0-9]+)?)`)
	vmafRE       = regexp.MustCompile(`VMAF score:\s*([-+]?[0-9]+(?:\.[0-9]+)?)`)
)

func ParseSSIM(stderr []byte) (float64, error) {
	matches := filterSSIMRE.FindAllSubmatch(stderr, -1)
	if len(matches) == 0 {
		return 0, errors.New("SSIM Y was not found")
	}
	v, err := strconv.ParseFloat(string(matches[len(matches)-1][1]), 64)
	if err != nil {
		return 0, fmt.Errorf("parse SSIM: %w", err)
	}
	return v, nil
}

func ParseVMAF(stderr []byte) (float64, error) {
	matches := vmafRE.FindAllSubmatch(stderr, -1)
	if len(matches) == 0 {
		return 0, errors.New("VMAF score was not found")
	}
	v, err := strconv.ParseFloat(string(matches[len(matches)-1][1]), 64)
	if err != nil {
		return 0, fmt.Errorf("parse VMAF: %w", err)
	}
	return v, nil
}

func (c *Client) MeasureQuality(ctx context.Context, input string, ordinal int, pixFmt, preset, encoder string, metric quality.Metric, start, duration float64, value int) (quality.Scores, error) {
	path, cleanup, err := temporaryMediaPath("vcompress-quality-sample-*.mkv")
	if err != nil {
		return quality.Scores{}, err
	}
	defer cleanup()

	normalizedEncoder := normalizeEncoder(encoder)
	_, stderr, err := c.runWithNVDECFallback(ctx, normalizedEncoder+" sample decode", normalizedEncoder == "hevc_nvenc", func(useNVDEC, keepFramesOnGPU bool) []string {
		args := []string{"-hide_banner", "-y", "-loglevel", "error", "-ss", fmt.Sprintf("%.3f", start)}
		args = appendHWAccel(args, useNVDEC, keepFramesOnGPU)
		args = append(args,
			"-i", input, "-t", fmt.Sprintf("%.3f", duration),
			"-map", fmt.Sprintf("0:v:%d", ordinal), "-an", "-sn", "-dn",
		)
		if normalizedEncoder == "hevc_nvenc" {
			args = append(args,
				"-c:v", "hevc_nvenc", "-preset", nvencPreset(preset), "-tune", "hq",
				"-rc", "vbr", "-cq", strconv.Itoa(value), "-b:v", "0",
			)
		} else {
			args = append(args, "-c:v", "libx265", "-preset", preset, "-crf", strconv.Itoa(value))
		}
		if !keepFramesOnGPU {
			args = append(args, "-pix_fmt", pixFmt)
		}
		args = append(args, "-fps_mode", "passthrough", path)
		return args
	})
	if err != nil {
		return quality.Scores{}, fmt.Errorf("%s sample encode failed: %w: %s", normalizedEncoder, err, tail(string(stderr), 12))
	}

	scores := quality.Scores{}
	if metric == quality.MetricVMAF || metric == quality.MetricBoth {
		stderr, err = c.compareSample(ctx, input, path, ordinal, start, duration, "libvmaf=model=version=vmaf_v0.6.1:shortest=1", "VMAF")
		if err != nil {
			return quality.Scores{}, err
		}
		scores.VMAF, err = ParseVMAF(stderr)
		if err != nil {
			return quality.Scores{}, err
		}
	}
	if metric == quality.MetricSSIM || metric == quality.MetricBoth {
		stderr, err = c.compareSample(ctx, input, path, ordinal, start, duration, "ssim=shortest=1", "SSIM")
		if err != nil {
			return quality.Scores{}, err
		}
		scores.SSIM, err = ParseSSIM(stderr)
		if err != nil {
			return quality.Scores{}, err
		}
	}
	return scores, nil
}

func (c *Client) compareSample(ctx context.Context, input, sample string, ordinal int, start, duration float64, filter, metricName string) ([]byte, error) {
	_, stderr, err := c.runWithNVDECFallback(ctx, metricName+" comparison decode", false, func(useNVDEC, keepFramesOnGPU bool) []string {
		args := []string{"-hide_banner", "-nostats", "-loglevel", "info", "-ss", fmt.Sprintf("%.3f", start), "-t", fmt.Sprintf("%.3f", duration)}
		args = appendHWAccel(args, useNVDEC, keepFramesOnGPU)
		args = append(args, "-i", input)
		args = appendHWAccel(args, useNVDEC, keepFramesOnGPU)
		return append(args,
			"-i", sample,
			"-filter_complex", fmt.Sprintf("[0:v:%d]setpts=PTS-STARTPTS[ref];[1:v:0]setpts=PTS-STARTPTS[dist];[dist][ref]%s", ordinal, filter),
			"-an", "-sn", "-dn", "-f", "null", "-",
		)
	})
	if err != nil {
		return nil, fmt.Errorf("%s comparison failed: %w: %s", metricName, err, tail(string(stderr), 12))
	}
	return stderr, nil
}

type EncodeOptions struct {
	Input      string
	Output     string
	Ordinal    int
	PixFmt     string
	Preset     string
	Quality    int
	ColorRange string
	ColorSpace string
	ColorTrc   string
	ColorPrim  string
	Encoder    string
}

func (c *Client) Encode(ctx context.Context, o EncodeOptions) error {
	ord := strconv.Itoa(o.Ordinal)
	encoder := normalizeEncoder(o.Encoder)
	_, stderr, err := c.runWithNVDECFallback(ctx, "final encode decode", encoder == "hevc_nvenc", func(useNVDEC, keepFramesOnGPU bool) []string {
		args := []string{"-hide_banner", "-y"}
		args = appendHWAccel(args, useNVDEC, keepFramesOnGPU)
		args = append(args, "-i", o.Input,
			"-map", "0", "-map_metadata", "0", "-map_chapters", "0",
			"-c", "copy",
			"-c:v:"+ord, encoder)
		if encoder == "hevc_nvenc" {
			args = append(args,
				"-preset:v:"+ord, nvencPreset(o.Preset), "-tune:v:"+ord, "hq",
				"-rc:v:"+ord, "vbr", "-cq:v:"+ord, strconv.Itoa(o.Quality), "-b:v:"+ord, "0",
			)
		} else {
			args = append(args, "-preset:v:"+ord, o.Preset, "-crf:v:"+ord, strconv.Itoa(o.Quality))
		}
		if !keepFramesOnGPU {
			args = append(args, "-pix_fmt:v:"+ord, o.PixFmt)
		}
		args = append(args, "-fps_mode:v:"+ord, "passthrough", "-max_muxing_queue_size", "4096")
		if o.ColorRange != "" && o.ColorRange != "unknown" {
			args = append(args, "-color_range:v:"+ord, o.ColorRange)
		}
		if o.ColorSpace != "" && o.ColorSpace != "unknown" {
			args = append(args, "-colorspace:v:"+ord, o.ColorSpace)
		}
		if o.ColorTrc != "" && o.ColorTrc != "unknown" {
			args = append(args, "-color_trc:v:"+ord, o.ColorTrc)
		}
		if o.ColorPrim != "" && o.ColorPrim != "unknown" {
			args = append(args, "-color_primaries:v:"+ord, o.ColorPrim)
		}
		switch strings.ToLower(filepath.Ext(o.Output)) {
		case ".mp4", ".m4v", ".mov":
			args = append(args, "-tag:v:"+ord, "hvc1", "-movflags", "+faststart")
		}
		return append(args, o.Output)
	})
	if err != nil {
		return fmt.Errorf("ffmpeg encode/mux failed: %w: %s", err, tail(string(stderr), 20))
	}
	return nil
}

func (c *Client) FullDecode(ctx context.Context, path string, ordinal int) error {
	_, stderr, err := c.runWithNVDECFallback(ctx, "full decode check", true, func(useNVDEC, keepFramesOnGPU bool) []string {
		args := []string{"-hide_banner", "-loglevel", "error", "-xerror"}
		args = appendHWAccel(args, useNVDEC, keepFramesOnGPU)
		return append(args, "-i", path, "-map", fmt.Sprintf("0:v:%d", ordinal), "-f", "null", "-")
	})
	if err != nil {
		return fmt.Errorf("full decode check failed: %w: %s", err, tail(string(stderr), 12))
	}
	return nil
}

func normalizeEncoder(encoder string) string {
	if encoder == "hevc_nvenc" {
		return encoder
	}
	return "libx265"
}

func nvencPreset(preset string) string {
	switch strings.ToLower(preset) {
	case "ultrafast", "superfast":
		return "p1"
	case "veryfast":
		return "p2"
	case "faster":
		return "p3"
	case "fast":
		return "p4"
	case "medium":
		return "p5"
	case "slow":
		return "p6"
	default:
		return "p7"
	}
}

func appendHWAccel(args []string, enabled, keepFramesOnGPU bool) []string {
	if enabled {
		args = append(args, "-hwaccel", "cuda")
		if keepFramesOnGPU {
			args = append(args, "-hwaccel_output_format", "cuda")
		}
	}
	return args
}

func (c *Client) runWithNVDECFallback(ctx context.Context, operation string, keepFramesOnGPU bool, args func(useNVDEC, keepFramesOnGPU bool) []string) ([]byte, []byte, error) {
	if !c.NVIDIA.NVDEC {
		return c.Runner.Run(ctx, c.FFmpeg, args(false, false)...)
	}
	stdout, stderr, err := c.Runner.Run(ctx, c.FFmpeg, args(true, keepFramesOnGPU)...)
	if err == nil || ctx.Err() != nil {
		return stdout, stderr, err
	}
	if c.Logf != nil {
		c.Logf("NVDEC-FALLBACK: %s failed; retrying with software decode | %s", operation, commandFailure(operation, err, stderr))
	}
	return c.Runner.Run(ctx, c.FFmpeg, args(false, false)...)
}

func temporaryMediaPath(pattern string) (string, func(), error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("create temporary media path: %w", err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", nil, fmt.Errorf("close temporary media path: %w", err)
	}
	cleanup := func() { _ = os.Remove(path) }
	return path, cleanup, nil
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimSpace(s), "\n")
	if len(parts) <= lines {
		return strings.TrimSpace(s)
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}
