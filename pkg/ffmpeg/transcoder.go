package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// HWAccel selects the H.264 encoder strategy.
type HWAccel string

const (
	// HWAccelAuto uses h264_videotoolbox on macOS and libx264 elsewhere.
	HWAccelAuto HWAccel = "auto"
	// HWAccelVideoToolbox forces Apple VideoToolbox (still falls back to
	// libx264 if the hardware encoder errors out).
	HWAccelVideoToolbox HWAccel = "videotoolbox"
	// HWAccelNone always uses software libx264.
	HWAccelNone HWAccel = "none"
)

const (
	encVideoToolbox = "h264_videotoolbox"
	encX264         = "libx264"
)

// Rendition is one rung of the adaptive bitrate ladder.
type Rendition struct {
	Name    string // used in playlist/segment filenames, e.g. "1080p"
	Height  int
	Bitrate string
	MaxRate string
	BufSize string
}

// DefaultLadder is the 1080p + 720p ABR ladder. Rungs taller than the source
// are dropped automatically so nothing is ever upscaled.
var DefaultLadder = []Rendition{
	{Name: "1080p", Height: 1080, Bitrate: "5000k", MaxRate: "5500k", BufSize: "11000k"},
	{Name: "720p", Height: 720, Bitrate: "2800k", MaxRate: "3100k", BufSize: "6200k"},
}

// HLSOptions tunes GenerateHLS.
type HLSOptions struct {
	// SegmentSeconds is the target .ts segment duration (default 6).
	SegmentSeconds int
	// DurationSeconds of the source; required for progress percentages.
	DurationSeconds float64
	// SourceHeight prunes ladder rungs taller than the source (0 = keep all).
	SourceHeight int
	// HasAudio must reflect the probed source; it shapes the stream mapping.
	HasAudio bool
	// HWAccel selects the encoder strategy (default HWAccelAuto).
	HWAccel HWAccel
	// Ladder overrides DefaultLadder when non-empty.
	Ladder []Rendition
	// OnProgress receives the encode percentage (0-100). Called from the
	// goroutine reading ffmpeg's progress stream; keep it fast.
	OnProgress func(percent float64)
}

// Transcoder produces multi-bitrate HLS renditions and thumbnails with real
// ffmpeg subprocesses.
type Transcoder struct {
	// FFmpegPath is the ffmpeg binary; defaults to "ffmpeg" on PATH.
	FFmpegPath string
	// Log receives encoder fallback notices; defaults to slog.Default().
	Log *slog.Logger
}

func NewTranscoder(ffmpegPath string, log *slog.Logger) *Transcoder {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if log == nil {
		log = slog.Default()
	}
	return &Transcoder{FFmpegPath: ffmpegPath, Log: log}
}

// GenerateHLS transcodes input into an adaptive multi-bitrate HLS VOD set
// inside outDir (flat layout):
//
//	master.m3u8              variant playlist index
//	index_1080p.m3u8         media playlist per rendition
//	segment_1080p_00042.ts   MPEG-TS segments
//
// Encoders are tried in order (VideoToolbox first on macOS, then libx264);
// a failed attempt wipes outDir before the next try, so no partial rendition
// ever survives. Progress is parsed from ffmpeg's machine-readable
// `-progress pipe:1` stream and reported via opts.OnProgress.
func (t *Transcoder) GenerateHLS(ctx context.Context, input, outDir string, opts HLSOptions) error {
	if opts.SegmentSeconds <= 0 {
		opts.SegmentSeconds = 6
	}
	ladder := buildLadder(opts)

	var lastErr error
	for _, encoder := range t.encoderCandidates(opts.HWAccel) {
		// Clean slate per attempt.
		if err := os.RemoveAll(outDir); err != nil {
			return fmt.Errorf("hls: clean output dir: %w", err)
		}
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("hls: create output dir: %w", err)
		}

		args := buildHLSArgs(input, outDir, encoder, ladder, opts)
		err := t.runWithProgress(ctx, args, opts.DurationSeconds, opts.OnProgress)
		if err == nil {
			if opts.OnProgress != nil {
				opts.OnProgress(100)
			}
			return nil
		}
		if ctx.Err() != nil {
			_ = os.RemoveAll(outDir)
			return fmt.Errorf("hls: %w", ctx.Err())
		}
		lastErr = err
		t.Log.Warn("hls encoder failed, trying next candidate", "encoder", encoder, "error", err)
	}

	_ = os.RemoveAll(outDir)
	return fmt.Errorf("hls: all encoders failed: %w", lastErr)
}

// encoderCandidates returns the encoders to try, in order. VideoToolbox is
// hardware-accelerated on Apple silicon; libx264 is the universal fallback,
// which also makes the same binary work inside Linux containers.
func (t *Transcoder) encoderCandidates(hw HWAccel) []string {
	switch hw {
	case HWAccelVideoToolbox:
		return []string{encVideoToolbox, encX264}
	case HWAccelNone:
		return []string{encX264}
	default: // HWAccelAuto
		if runtime.GOOS == "darwin" {
			return []string{encVideoToolbox, encX264}
		}
		return []string{encX264}
	}
}

// buildLadder prunes rungs taller than the source; if every rung is pruned
// (e.g. a 480p phone clip) a single rendition at source height is used.
func buildLadder(opts HLSOptions) []Rendition {
	src := opts.Ladder
	if len(src) == 0 {
		src = DefaultLadder
	}
	if opts.SourceHeight <= 0 {
		return src
	}
	var out []Rendition
	for _, r := range src {
		if r.Height <= opts.SourceHeight {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		h := opts.SourceHeight - opts.SourceHeight%2 // encoder needs even dims
		out = []Rendition{{
			Name:    fmt.Sprintf("%dp", h),
			Height:  h,
			Bitrate: "1800k", MaxRate: "2000k", BufSize: "4000k",
		}}
	}
	return out
}

// buildHLSArgs assembles the full ffmpeg invocation: one input decoded once,
// split and scaled per rendition via filter_complex, and muxed by the HLS
// muxer with a generated master playlist.
func buildHLSArgs(input, outDir, encoder string, ladder []Rendition, opts HLSOptions) []string {
	n := len(ladder)

	// [0:v]split=2[vt0][vt1]; [vt0]scale=-2:min(1080,ih)[v0]; ...
	var graph strings.Builder
	graph.WriteString(fmt.Sprintf("[0:v]split=%d", n))
	for i := range ladder {
		graph.WriteString(fmt.Sprintf("[vt%d]", i))
	}
	for i, r := range ladder {
		// min(H,ih) double-guards against upscaling; \, escapes the comma
		// inside the filter expression.
		graph.WriteString(fmt.Sprintf(";[vt%d]scale=-2:min(%d\\,ih)[v%d]", i, r.Height, i))
	}

	args := []string{
		"-hide_banner", "-nostdin", "-y",
		"-loglevel", "error",
		"-i", input,
		"-filter_complex", graph.String(),
	}

	for i := range ladder {
		args = append(args, "-map", fmt.Sprintf("[v%d]", i))
	}
	if opts.HasAudio {
		// One audio stream per variant, as required by -var_stream_map.
		for range ladder {
			args = append(args, "-map", "a:0")
		}
	}

	for i, r := range ladder {
		args = append(args,
			fmt.Sprintf("-c:v:%d", i), encoder,
			fmt.Sprintf("-b:v:%d", i), r.Bitrate,
			fmt.Sprintf("-maxrate:v:%d", i), r.MaxRate,
			fmt.Sprintf("-bufsize:v:%d", i), r.BufSize,
		)
	}

	args = append(args, "-pix_fmt", "yuv420p")
	// Segment-aligned keyframes so every .ts starts on an IDR frame and
	// players can switch renditions cleanly at segment boundaries.
	args = append(args, "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", opts.SegmentSeconds))

	switch encoder {
	case encX264:
		args = append(args, "-preset", "veryfast")
	case encVideoToolbox:
		// Permit VideoToolbox's own software path on machines whose hardware
		// encoder rejects the format; a hard failure still falls back to x264.
		args = append(args, "-allow_sw", "1")
	}

	if opts.HasAudio {
		args = append(args, "-c:a", "aac", "-b:a", "128k", "-ac", "2")
	}

	var streamMap strings.Builder
	for i, r := range ladder {
		if i > 0 {
			streamMap.WriteString(" ")
		}
		if opts.HasAudio {
			streamMap.WriteString(fmt.Sprintf("v:%d,a:%d,name:%s", i, i, r.Name))
		} else {
			streamMap.WriteString(fmt.Sprintf("v:%d,name:%s", i, r.Name))
		}
	}

	args = append(args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(opts.SegmentSeconds),
		"-hls_playlist_type", "vod",
		"-hls_flags", "independent_segments",
		"-hls_segment_filename", filepath.Join(outDir, "segment_%v_%05d.ts"),
		"-master_pl_name", "master.m3u8",
		"-var_stream_map", streamMap.String(),
		"-progress", "pipe:1", "-nostats",
		filepath.Join(outDir, "index_%v.m3u8"),
	)
	return args
}

// runWithProgress executes ffmpeg, parsing the key=value progress stream on
// stdout while capturing stderr for error reporting. Cancelling ctx kills
// the subprocess.
func (t *Transcoder) runWithProgress(ctx context.Context, args []string, durationSeconds float64, onProgress func(float64)) error {
	cmd := exec.CommandContext(ctx, t.FFmpegPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}

	// ffmpeg -progress emits blocks of key=value lines roughly twice per
	// second; out_time_us is the presentation timestamp encoded so far.
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok {
			continue
		}
		switch key {
		case "out_time_us", "out_time_ms": // out_time_ms is also microseconds (ffmpeg quirk)
			us, err := strconv.ParseInt(value, 10, 64)
			if err != nil || us <= 0 || durationSeconds <= 0 || onProgress == nil {
				continue
			}
			pct := float64(us) / 1e6 / durationSeconds * 100
			if pct > 99.5 {
				pct = 99.5 // 100 is reported only after a successful exit
			}
			onProgress(pct)
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("ffmpeg: %w (stderr: %s)", err, tail(stderr.Bytes()))
	}
	return nil
}

// VideoThumbnail extracts a single frame at atSeconds, scaled to maxWidth,
// and writes it as JPEG to output.
func (t *Transcoder) VideoThumbnail(ctx context.Context, input, output string, atSeconds float64, maxWidth int) error {
	if maxWidth <= 0 {
		maxWidth = 480
	}
	if atSeconds < 0 {
		atSeconds = 0
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("video thumbnail: create dir: %w", err)
	}
	_, err := run(ctx, t.FFmpegPath,
		"-hide_banner", "-nostdin", "-y",
		"-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64),
		"-i", input,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", maxWidth),
		"-q:v", "3",
		output,
	)
	if err != nil {
		return fmt.Errorf("video thumbnail: %w", err)
	}
	return nil
}

// ImageThumbnail downscales an image to maxWidth and writes it as JPEG.
func (t *Transcoder) ImageThumbnail(ctx context.Context, input, output string, maxWidth int) error {
	if maxWidth <= 0 {
		maxWidth = 480
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("image thumbnail: create dir: %w", err)
	}
	_, err := run(ctx, t.FFmpegPath,
		"-hide_banner", "-nostdin", "-y",
		"-i", input,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", maxWidth),
		"-q:v", "3",
		output,
	)
	if err != nil {
		return fmt.Errorf("image thumbnail: %w", err)
	}
	return nil
}

// ExtractCoverArt pulls the embedded cover picture out of an audio file
// (MP3/FLAC/M4A) and writes it as JPEG to output. Callers should first check
// ProbeResult.HasCoverArt.
func (t *Transcoder) ExtractCoverArt(ctx context.Context, input, output string, maxWidth int) error {
	if maxWidth <= 0 {
		maxWidth = 480
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("cover art: create dir: %w", err)
	}
	_, err := run(ctx, t.FFmpegPath,
		"-hide_banner", "-nostdin", "-y",
		"-i", input,
		"-map", "0:v:0",
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", maxWidth),
		"-q:v", "3",
		output,
	)
	if err != nil {
		return fmt.Errorf("cover art: %w", err)
	}
	return nil
}
