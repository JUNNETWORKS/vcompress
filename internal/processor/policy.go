package processor

type CodecPolicy int

const (
	Transcode CodecPolicy = iota
	SkipEfficient
	SkipArchivalOrUnknown
)

func PolicyForCodec(codec string) CodecPolicy {
	switch codec {
	case "h264", "mpeg4", "mpeg2video", "mpeg1video", "msmpeg4", "msmpeg4v1", "msmpeg4v2", "msmpeg4v3",
		"wmv1", "wmv2", "wmv3", "vc1", "vp8", "theora", "flv1", "h263", "h263p", "mjpeg":
		return Transcode
	case "hevc", "av1", "vp9", "vvc":
		return SkipEfficient
	case "prores", "dnxhd", "ffv1", "huffyuv", "utvideo", "rawvideo", "cfhd", "magicyuv", "jpeg2000":
		return SkipArchivalOrUnknown
	default:
		return SkipArchivalOrUnknown
	}
}
