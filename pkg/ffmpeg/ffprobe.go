package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ProbeResult is the normalized output of ffprobe for one media file.
type ProbeResult struct {
	DurationSeconds float64
	BitrateBps      int64
	Width           int
	Height          int
	VideoCodec      string
	AudioCodec      string
	SampleRate      int
	Channels        int
	FrameRate       float64
	HasVideo        bool
	HasAudio        bool
	// HasCoverArt is true when the file carries an attached picture stream
	// (typical for MP3/FLAC/M4A cover art).
	HasCoverArt bool
	Tags        map[string]string
}

// Prober extracts technical metadata from media files using ffprobe.
type Prober struct {
	// FFprobePath is the ffprobe binary; defaults to "ffprobe" on PATH.
	FFprobePath string
}

func NewProber(ffprobePath string) *Prober {
	if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}
	return &Prober{FFprobePath: ffprobePath}
}

// ffprobeOutput mirrors the subset of `ffprobe -print_format json` we use.
// ffprobe emits numeric fields as JSON strings, hence the string types.
type ffprobeOutput struct {
	Format struct {
		Duration string            `json:"duration"`
		BitRate  string            `json:"bit_rate"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		SampleRate  string `json:"sample_rate"`
		Channels    int    `json:"channels"`
		RFrameRate  string `json:"r_frame_rate"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
}

// Probe runs ffprobe against path and returns structured metadata. It works
// for video, audio and image files alike.
func (p *Prober) Probe(ctx context.Context, path string) (*ProbeResult, error) {
	out, err := run(ctx, p.FFprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	if err != nil {
		return nil, fmt.Errorf("probe %s: %w", path, err)
	}

	var raw ffprobeOutput
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("probe %s: parse ffprobe output: %w", path, err)
	}

	res := &ProbeResult{
		DurationSeconds: parseFloat(raw.Format.Duration),
		BitrateBps:      parseInt(raw.Format.BitRate),
		Tags:            normalizeTags(raw.Format.Tags),
	}

	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			if s.Disposition.AttachedPic == 1 {
				res.HasCoverArt = true
				continue
			}
			// Keep the first real video stream.
			if !res.HasVideo {
				res.HasVideo = true
				res.VideoCodec = s.CodecName
				res.Width = s.Width
				res.Height = s.Height
				res.FrameRate = parseFraction(s.RFrameRate)
			}
		case "audio":
			if !res.HasAudio {
				res.HasAudio = true
				res.AudioCodec = s.CodecName
				res.SampleRate = int(parseInt(s.SampleRate))
				res.Channels = s.Channels
			}
		}
	}
	return res, nil
}

func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

func parseInt(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseFraction parses ffprobe rate strings like "30000/1001" or "25/1".
func parseFraction(s string) float64 {
	num, den, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok {
		return parseFloat(s)
	}
	n := parseFloat(num)
	d := parseFloat(den)
	if d == 0 {
		return 0
	}
	return n / d
}

// normalizeTags lowercases tag keys so callers can rely on "artist", "album",
// "title" regardless of the container's casing conventions.
func normalizeTags(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}
