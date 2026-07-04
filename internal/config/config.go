package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"time"
)

// Config holds every runtime setting, populated from environment variables.
type Config struct {
	Port   int
	WebDir string

	DatabaseURL string

	StoragePath string

	JWTSecret          string
	JWTSecretGenerated bool // true when JWT_SECRET was empty and we minted one
	TokenTTL           time.Duration

	MaxWorkers        int
	JobQueueSize      int
	ProcessTimeout    time.Duration
	MaxUploadBytes    int64
	HLSSegmentSeconds int
	// HWAccel selects the H.264 encoder: "auto" (VideoToolbox on macOS,
	// libx264 elsewhere), "videotoolbox", or "none".
	HWAccel string
	// UploadTTL is how long an unfinished chunked upload survives before the
	// janitor reclaims its staging file.
	UploadTTL time.Duration

	FFmpegPath  string
	FFprobePath string
}

// Load reads configuration from the environment, applying safe defaults for a
// low-spec home server. It fails fast on values that cannot possibly work.
func Load() (*Config, error) {
	cfg := &Config{
		Port:              getInt("PORT", 8080),
		WebDir:            getStr("WEB_DIR", "./static"),
		DatabaseURL:       getStr("DATABASE_URL", "postgres://mediauser:mediapass@localhost:5432/mediacloud?sslmode=disable"),
		StoragePath:       getStr("STORAGE_PATH", "./data/media"),
		JWTSecret:         getStr("JWT_SECRET", ""),
		TokenTTL:          getDuration("TOKEN_TTL", 72*time.Hour),
		MaxWorkers:        getInt("MAX_WORKERS", defaultWorkers()),
		JobQueueSize:      getInt("JOB_QUEUE_SIZE", 128),
		ProcessTimeout:    getDuration("PROCESS_TIMEOUT", 45*time.Minute),
		MaxUploadBytes:    getInt64("MAX_UPLOAD_BYTES", 10<<30), // 10 GiB
		HLSSegmentSeconds: getInt("HLS_SEGMENT_SECONDS", 6),
		HWAccel:           getStr("HWACCEL", "auto"),
		UploadTTL:         getDuration("UPLOAD_TTL", 48*time.Hour),
		FFmpegPath:        getStr("FFMPEG_PATH", "ffmpeg"),
		FFprobePath:       getStr("FFPROBE_PATH", "ffprobe"),
	}

	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("config: PORT %d out of range", cfg.Port)
	}
	if cfg.MaxWorkers < 1 {
		return nil, fmt.Errorf("config: MAX_WORKERS must be >= 1, got %d", cfg.MaxWorkers)
	}
	if cfg.JobQueueSize < 1 {
		return nil, fmt.Errorf("config: JOB_QUEUE_SIZE must be >= 1, got %d", cfg.JobQueueSize)
	}
	if cfg.MaxUploadBytes < 1 {
		return nil, fmt.Errorf("config: MAX_UPLOAD_BYTES must be >= 1, got %d", cfg.MaxUploadBytes)
	}
	if cfg.HLSSegmentSeconds < 1 {
		return nil, fmt.Errorf("config: HLS_SEGMENT_SECONDS must be >= 1, got %d", cfg.HLSSegmentSeconds)
	}
	switch cfg.HWAccel {
	case "auto", "videotoolbox", "none":
	default:
		return nil, fmt.Errorf("config: HWACCEL must be auto, videotoolbox or none, got %q", cfg.HWAccel)
	}
	if cfg.JWTSecret == "" {
		secret, err := randomSecret()
		if err != nil {
			return nil, fmt.Errorf("config: generate fallback JWT secret: %w", err)
		}
		cfg.JWTSecret = secret
		cfg.JWTSecretGenerated = true
	}
	return cfg, nil
}

// defaultWorkers leaves headroom for the HTTP server and PostgreSQL on
// shared hardware: half the cores, at least one.
func defaultWorkers() int {
	n := runtime.NumCPU() / 2
	if n < 1 {
		n = 1
	}
	return n
}

func randomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func getStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
