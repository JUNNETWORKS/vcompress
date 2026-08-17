package ffmpeg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"vcompress/internal/media"
)

type Client struct {
	FFmpeg  string
	FFprobe string
	Runner  Runner
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

var ssimRE = regexp.MustCompile(`SSIM Mean Y:\s*([0-9]+(?:\.[0-9]+)?)`)

func ParseSSIM(stderr []byte) (float64, error) {
	matches := ssimRE.FindAllSubmatch(stderr, -1)
	if len(matches) == 0 {
		return 0, errors.New("x265 SSIM Mean Y not found")
	}
	v, err := strconv.ParseFloat(string(matches[len(matches)-1][1]), 64)
	if err != nil {
		return 0, fmt.Errorf("parse SSIM: %w", err)
	}
	return v, nil
}

func (c *Client) MeasureSSIM(ctx context.Context, input string, ordinal int, pixFmt, preset string, start, duration float64, crf int) (float64, error) {
	_, stderr, err := c.Runner.Run(ctx, c.FFmpeg,
		"-hide_banner", "-y", "-loglevel", "error",
		"-ss", fmt.Sprintf("%.3f", start), "-i", input,
		"-t", fmt.Sprintf("%.3f", duration),
		"-map", fmt.Sprintf("0:v:%d", ordinal), "-an", "-sn", "-dn",
		"-c:v", "libx265", "-preset", preset, "-crf", strconv.Itoa(crf), "-pix_fmt", pixFmt,
		"-x265-params", "ssim=1", "-f", "null", "-",
	)
	if err != nil {
		return 0, fmt.Errorf("sample encode failed: %w: %s", err, tail(string(stderr), 12))
	}
	return ParseSSIM(stderr)
}

type EncodeOptions struct {
	Input      string
	Output     string
	Ordinal    int
	PixFmt     string
	Preset     string
	CRF        int
	ColorRange string
	ColorSpace string
	ColorTrc   string
	ColorPrim  string
}

func (c *Client) Encode(ctx context.Context, o EncodeOptions) error {
	ord := strconv.Itoa(o.Ordinal)
	args := []string{
		"-hide_banner", "-y", "-i", o.Input,
		"-map", "0", "-map_metadata", "0", "-map_chapters", "0",
		"-c", "copy",
		"-c:v:" + ord, "libx265",
		"-preset:v:" + ord, o.Preset,
		"-crf:v:" + ord, strconv.Itoa(o.CRF),
		"-pix_fmt:v:" + ord, o.PixFmt,
		"-max_muxing_queue_size", "4096",
	}
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
	args = append(args, o.Output)
	_, stderr, err := c.Runner.Run(ctx, c.FFmpeg, args...)
	if err != nil {
		return fmt.Errorf("ffmpeg encode/mux failed: %w: %s", err, tail(string(stderr), 20))
	}
	return nil
}

func (c *Client) FullDecode(ctx context.Context, path string, ordinal int) error {
	_, stderr, err := c.Runner.Run(ctx, c.FFmpeg,
		"-hide_banner", "-loglevel", "error", "-xerror", "-i", path,
		"-map", fmt.Sprintf("0:v:%d", ordinal), "-f", "null", "-",
	)
	if err != nil {
		return fmt.Errorf("full decode check failed: %w: %s", err, tail(string(stderr), 12))
	}
	return nil
}

func tail(s string, lines int) string {
	parts := strings.Split(strings.TrimSpace(s), "\n")
	if len(parts) <= lines {
		return strings.TrimSpace(s)
	}
	return strings.Join(parts[len(parts)-lines:], "\n")
}
