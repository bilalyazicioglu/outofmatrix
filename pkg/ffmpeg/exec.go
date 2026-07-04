// Package ffmpeg wraps the ffmpeg and ffprobe binaries behind small,
// context-aware Go APIs. It has no dependency on the rest of the application,
// so it can be reused or extracted as a standalone library.
package ffmpeg

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// stderrTailLimit bounds how much ffmpeg stderr we keep for error messages.
const stderrTailLimit = 4096

// run executes a binary with the given arguments, returning stdout. On
// failure the error includes the tail of stderr, which is where ffmpeg puts
// everything useful. Cancelling ctx kills the subprocess.
func run(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("%s: %w", bin, ctxErr)
		}
		return nil, fmt.Errorf("%s %v: %w (stderr: %s)", bin, args, err, tail(stderr.Bytes()))
	}
	return stdout.Bytes(), nil
}

func tail(b []byte) []byte {
	if len(b) > stderrTailLimit {
		return b[len(b)-stderrTailLimit:]
	}
	return b
}
