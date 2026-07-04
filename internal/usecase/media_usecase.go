package usecase

import (
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/buckket/go-blurhash"
	"github.com/google/uuid"

	"outofmatrix/internal/domain"
	"outofmatrix/internal/worker"
	"outofmatrix/pkg/ffmpeg"
)

// Storage layout under the storage root:
//
//	originals/<id><ext>     uploaded files, untouched
//	thumbs/<id>.jpg         jpeg thumbnails for the grid UI
//	hls/<id>/master.m3u8    adaptive multi-bitrate HLS set per video
//	uploads/<session>.part  in-flight chunked uploads
const (
	dirOriginals = "originals"
	dirThumbs    = "thumbs"
	dirHLS       = "hls"
	dirUploads   = "uploads"

	thumbnailMaxWidth = 480
)

// Overall progress budget per pipeline stage (percent).
const (
	progressProbe     = 3.0
	progressThumbnail = 8.0
	// Transcoding owns the remaining 8..100 band.
)

// MetadataProber and MediaTranscoder are the FFmpeg ports of this usecase;
// the concrete implementations live in pkg/ffmpeg.
type MetadataProber interface {
	Probe(ctx context.Context, path string) (*ffmpeg.ProbeResult, error)
}

type MediaTranscoder interface {
	GenerateHLS(ctx context.Context, input, outDir string, opts ffmpeg.HLSOptions) error
	VideoThumbnail(ctx context.Context, input, output string, atSeconds float64, maxWidth int) error
	ImageThumbnail(ctx context.Context, input, output string, maxWidth int) error
	ExtractCoverArt(ctx context.Context, input, output string, maxWidth int) error
}

// Dispatcher queues background processing jobs; the worker pool implements it.
type Dispatcher interface {
	Enqueue(job worker.Job) error
}

// MediaConfig wires a MediaUsecase.
type MediaConfig struct {
	Repo              domain.MediaRepository
	Prober            MetadataProber
	Transcoder        MediaTranscoder
	Dispatcher        Dispatcher
	Notifier          Notifier // optional; nil disables push events
	StoragePath       string
	HLSSegmentSeconds int
	HWAccel           ffmpeg.HWAccel
	Log               *slog.Logger
}

// MediaUsecase orchestrates upload, background processing and lifecycle of
// media items, emitting real-time events through the Notifier as work moves
// through the pipeline.
type MediaUsecase struct {
	repo       domain.MediaRepository
	prober     MetadataProber
	transcoder MediaTranscoder
	dispatcher Dispatcher
	notifier   Notifier
	storage    string
	segmentSec int
	hwAccel    ffmpeg.HWAccel
	log        *slog.Logger
}

func NewMediaUsecase(cfg MediaConfig) *MediaUsecase {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Notifier == nil {
		cfg.Notifier = noopNotifier{}
	}
	if cfg.HWAccel == "" {
		cfg.HWAccel = ffmpeg.HWAccelAuto
	}
	return &MediaUsecase{
		repo:       cfg.Repo,
		prober:     cfg.Prober,
		transcoder: cfg.Transcoder,
		dispatcher: cfg.Dispatcher,
		notifier:   cfg.Notifier,
		storage:    cfg.StoragePath,
		segmentSec: cfg.HLSSegmentSeconds,
		hwAccel:    cfg.HWAccel,
		log:        cfg.Log,
	}
}

// EnsureStorageDirs creates the storage layout; call once at boot.
func (u *MediaUsecase) EnsureStorageDirs() error {
	for _, d := range []string{dirOriginals, dirThumbs, dirHLS, dirUploads} {
		if err := os.MkdirAll(filepath.Join(u.storage, d), 0o755); err != nil {
			return fmt.Errorf("media: create storage dir %s: %w", d, err)
		}
	}
	return nil
}

// AbsPath resolves a stored relative path against the storage root.
func (u *MediaUsecase) AbsPath(rel string) string {
	return filepath.Join(u.storage, rel)
}

// UploadStagingPath is where a chunked upload session accumulates its bytes.
func (u *MediaUsecase) UploadStagingPath(sessionID uuid.UUID) string {
	return filepath.Join(u.storage, dirUploads, sessionID.String()+".part")
}

func (u *MediaUsecase) notify(item *domain.MediaItem, status, stage string, progress float64, errMsg string) {
	u.notifier.NotifyMedia(item.UserID, MediaEvent{
		MediaID:  item.ID,
		Title:    item.Title,
		Type:     item.Type,
		Status:   status,
		Stage:    stage,
		Progress: progress,
		Error:    errMsg,
	})
}

// CreateFromUpload streams src to disk (never buffering the file in RAM),
// persists the MediaItem and queues it for background processing.
func (u *MediaUsecase) CreateFromUpload(ctx context.Context, userID uuid.UUID, title, filename, mimeType string, src io.Reader) (*domain.MediaItem, error) {
	mediaType, err := domain.MediaTypeFromMime(mimeType)
	if err != nil {
		return nil, err
	}

	id := uuid.New()
	relPath := filepath.Join(dirOriginals, id.String()+sanitizeExt(filename))
	absPath := u.AbsPath(relPath)

	f, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("media: create file: %w", err)
	}
	size, err := io.Copy(f, src)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(absPath)
		return nil, fmt.Errorf("media: write upload: %w", err)
	}
	if size == 0 {
		_ = os.Remove(absPath)
		return nil, fmt.Errorf("%w: uploaded file is empty", domain.ErrInvalidInput)
	}

	return u.persistAndEnqueue(ctx, id, userID, title, filename, mimeType, mediaType, relPath, size)
}

// CreateFromFile registers an already-staged file (a completed chunked
// upload) as a MediaItem. The file is moved — not copied — into the
// originals directory, so completing a 5 GB upload is O(1).
func (u *MediaUsecase) CreateFromFile(ctx context.Context, userID uuid.UUID, title, filename, mimeType, stagedPath string) (*domain.MediaItem, error) {
	mediaType, err := domain.MediaTypeFromMime(mimeType)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(stagedPath)
	if err != nil {
		return nil, fmt.Errorf("media: stat staged file: %w", err)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("%w: staged file is empty", domain.ErrInvalidInput)
	}

	id := uuid.New()
	relPath := filepath.Join(dirOriginals, id.String()+sanitizeExt(filename))
	absPath := u.AbsPath(relPath)
	// Same filesystem (both live under the storage root), so this is a rename.
	if err := os.Rename(stagedPath, absPath); err != nil {
		return nil, fmt.Errorf("media: move staged file: %w", err)
	}

	return u.persistAndEnqueue(ctx, id, userID, title, filename, mimeType, mediaType, relPath, info.Size())
}

// persistAndEnqueue is the shared tail of both upload paths: save the row as
// pending, queue the background job, announce it over WebSocket.
func (u *MediaUsecase) persistAndEnqueue(ctx context.Context, id, userID uuid.UUID, title, filename, mimeType string, mediaType domain.MediaType, relPath string, size int64) (*domain.MediaItem, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		base := filepath.Base(filename)
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if title == "" {
		title = id.String()
	}

	now := time.Now().UTC()
	item := &domain.MediaItem{
		ID:        id,
		UserID:    userID,
		Title:     title,
		FilePath:  relPath,
		Type:      mediaType,
		Status:    domain.MediaStatusPending,
		FileSize:  size,
		MimeType:  mimeType,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := u.repo.Save(ctx, item); err != nil {
		_ = os.Remove(u.AbsPath(relPath))
		return nil, err
	}

	if err := u.dispatcher.Enqueue(worker.Job{MediaID: id}); err != nil {
		// Not fatal: the item is durably "pending" and RecoverUnfinished will
		// pick it up on the next boot (or a manual reprocess).
		u.log.Warn("processing queue full; item left pending", "media_id", id, "error", err)
	}
	u.notify(item, EventQueued, "", 0, "")
	return item, nil
}

// Process is the worker-pool handler: it extracts metadata and generates
// derivatives for one media item, streaming progress events along the way.
func (u *MediaUsecase) Process(ctx context.Context, mediaID uuid.UUID) error {
	item, err := u.repo.FindByID(ctx, mediaID)
	if err != nil {
		return err
	}
	if item.Status == domain.MediaStatusReady {
		return nil // already done (e.g. duplicate job after crash recovery)
	}

	item.Status = domain.MediaStatusProcessing
	item.UpdatedAt = time.Now().UTC()
	if err := u.repo.Update(ctx, item); err != nil {
		return err
	}
	u.notify(item, EventProcessing, "probe", 0, "")

	if err := u.buildDerivatives(ctx, item); err != nil {
		item.Status = domain.MediaStatusFailed
		item.Metadata.ProcessingError = err.Error()
		item.UpdatedAt = time.Now().UTC()
		// The job context may already be cancelled or timed out; record the
		// failure with a detached context so the status is not lost.
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if uerr := u.repo.Update(saveCtx, item); uerr != nil {
			u.log.Error("failed to persist failure status", "media_id", item.ID, "error", uerr)
		}
		u.notify(item, EventFailed, "", 0, err.Error())
		return fmt.Errorf("media: process %s: %w", item.ID, err)
	}

	item.Status = domain.MediaStatusReady
	item.Metadata.ProcessingError = ""
	item.UpdatedAt = time.Now().UTC()
	if err := u.repo.Update(ctx, item); err != nil {
		return err
	}
	u.notify(item, EventCompleted, "", 100, "")
	return nil
}

// buildDerivatives mutates item in place with probe metadata, thumbnail,
// blurhash and (for videos) the adaptive HLS set.
func (u *MediaUsecase) buildDerivatives(ctx context.Context, item *domain.MediaItem) error {
	srcAbs := u.AbsPath(item.FilePath)

	probe, err := u.prober.Probe(ctx, srcAbs)
	if err != nil {
		return err
	}
	item.Metadata = domain.MediaMetadata{
		DurationSeconds: probe.DurationSeconds,
		Width:           probe.Width,
		Height:          probe.Height,
		VideoCodec:      probe.VideoCodec,
		AudioCodec:      probe.AudioCodec,
		BitrateBps:      probe.BitrateBps,
		SampleRate:      probe.SampleRate,
		Channels:        probe.Channels,
		FrameRate:       probe.FrameRate,
		Tags:            probe.Tags,
	}
	u.notify(item, EventProcessing, "probe", progressProbe, "")

	thumbRel := filepath.Join(dirThumbs, item.ID.String()+".jpg")
	thumbAbs := u.AbsPath(thumbRel)

	switch item.Type {
	case domain.MediaTypePhoto:
		if err := u.transcoder.ImageThumbnail(ctx, srcAbs, thumbAbs, thumbnailMaxWidth); err != nil {
			return err
		}
		item.ThumbnailPath = thumbRel
		item.BlurHash = u.blurHashFromJPEG(thumbAbs)

	case domain.MediaTypeVideo:
		// Grab the poster frame at 10% in, clamped so very short clips and
		// unknown durations still work.
		at := probe.DurationSeconds * 0.10
		if at < 0.5 {
			at = 0
		}
		if err := u.transcoder.VideoThumbnail(ctx, srcAbs, thumbAbs, at, thumbnailMaxWidth); err != nil {
			return err
		}
		item.ThumbnailPath = thumbRel
		item.BlurHash = u.blurHashFromJPEG(thumbAbs)
		u.notify(item, EventProcessing, "thumbnail", progressThumbnail, "")

		hlsRel := filepath.Join(dirHLS, item.ID.String())
		lastSent := progressThumbnail
		opts := ffmpeg.HLSOptions{
			SegmentSeconds:  u.segmentSec,
			DurationSeconds: probe.DurationSeconds,
			SourceHeight:    probe.Height,
			HasAudio:        probe.HasAudio,
			HWAccel:         u.hwAccel,
			OnProgress: func(pct float64) {
				// Map the encode's 0-100 into the pipeline's 8-100 band and
				// throttle to whole-percent steps to keep WebSocket traffic sane.
				overall := progressThumbnail + pct*(100-progressThumbnail)/100
				if overall-lastSent >= 1 || pct >= 100 {
					lastSent = overall
					u.notify(item, EventProcessing, "transcode", overall, "")
				}
			},
		}
		if err := u.transcoder.GenerateHLS(ctx, srcAbs, u.AbsPath(hlsRel), opts); err != nil {
			return err
		}
		item.HLSPath = hlsRel

	case domain.MediaTypeAudio:
		if probe.HasCoverArt {
			if err := u.transcoder.ExtractCoverArt(ctx, srcAbs, thumbAbs, thumbnailMaxWidth); err != nil {
				// Cover art is decorative; a broken tag must not fail the track.
				u.log.Warn("cover art extraction failed", "media_id", item.ID, "error", err)
			} else {
				item.ThumbnailPath = thumbRel
				item.BlurHash = u.blurHashFromJPEG(thumbAbs)
			}
		}

	default:
		return fmt.Errorf("%w: unknown media type %q", domain.ErrInvalidInput, item.Type)
	}
	return nil
}

// blurHashFromJPEG computes the BlurHash placeholder from an on-disk JPEG
// thumbnail. Failures are logged and return an empty hash: a missing
// placeholder never blocks the pipeline.
func (u *MediaUsecase) blurHashFromJPEG(path string) string {
	f, err := os.Open(path)
	if err != nil {
		u.log.Warn("blurhash: open thumbnail", "path", path, "error", err)
		return ""
	}
	defer f.Close()

	img, err := jpeg.Decode(f)
	if err != nil {
		u.log.Warn("blurhash: decode thumbnail", "path", path, "error", err)
		return ""
	}
	hash, err := blurhash.Encode(4, 3, img)
	if err != nil {
		u.log.Warn("blurhash: encode", "path", path, "error", err)
		return ""
	}
	return hash
}

// RecoverUnfinished re-queues items that were pending or mid-processing when
// the server last stopped. Call once at boot after the pool has started.
func (u *MediaUsecase) RecoverUnfinished(ctx context.Context) (int, error) {
	ids, err := u.repo.ListIDsByStatus(ctx,
		[]domain.MediaStatus{domain.MediaStatusPending, domain.MediaStatusProcessing}, 1000)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, id := range ids {
		if err := u.dispatcher.Enqueue(worker.Job{MediaID: id}); err != nil {
			u.log.Warn("recovery: queue full, remaining items stay pending", "queued", queued, "total", len(ids))
			break
		}
		queued++
	}
	return queued, nil
}

// Get returns one media item, enforcing ownership (admins see everything).
func (u *MediaUsecase) Get(ctx context.Context, requesterID uuid.UUID, requesterRole domain.Role, id uuid.UUID) (*domain.MediaItem, error) {
	item, err := u.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item.UserID != requesterID && requesterRole != domain.RoleAdmin {
		return nil, fmt.Errorf("%w: media %s", domain.ErrForbidden, id)
	}
	return item, nil
}

// List returns one page of the requester's own media plus the total count.
func (u *MediaUsecase) List(ctx context.Context, userID uuid.UUID, mediaType domain.MediaType, limit, offset int) ([]*domain.MediaItem, int64, error) {
	if mediaType != "" && !mediaType.Valid() {
		return nil, 0, fmt.Errorf("%w: invalid media type %q", domain.ErrInvalidInput, mediaType)
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	items, err := u.repo.ListByUserID(ctx, userID, mediaType, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := u.repo.CountByUserID(ctx, userID, mediaType)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Delete removes the database row and every file derived from the item.
func (u *MediaUsecase) Delete(ctx context.Context, requesterID uuid.UUID, requesterRole domain.Role, id uuid.UUID) error {
	item, err := u.Get(ctx, requesterID, requesterRole, id)
	if err != nil {
		return err
	}
	if err := u.repo.Delete(ctx, id); err != nil {
		return err
	}
	// Files go last: if a removal fails we only leak disk space, never a
	// database row pointing at nothing.
	if err := os.Remove(u.AbsPath(item.FilePath)); err != nil && !os.IsNotExist(err) {
		u.log.Warn("delete: original not removed", "media_id", id, "error", err)
	}
	if item.ThumbnailPath != "" {
		if err := os.Remove(u.AbsPath(item.ThumbnailPath)); err != nil && !os.IsNotExist(err) {
			u.log.Warn("delete: thumbnail not removed", "media_id", id, "error", err)
		}
	}
	if item.HLSPath != "" {
		if err := os.RemoveAll(u.AbsPath(item.HLSPath)); err != nil {
			u.log.Warn("delete: hls dir not removed", "media_id", id, "error", err)
		}
	}
	return nil
}

// extPattern accepts a dot followed by 1-8 alphanumerics ("jpg", "m4a", ...).
var extPattern = regexp.MustCompile(`^\.[a-zA-Z0-9]{1,8}$`)

// sanitizeExt extracts a safe, lowercase file extension from a client
// filename; anything suspicious becomes ".bin". Stored names are always
// "<uuid><ext>", so client input can never influence directories.
func sanitizeExt(filename string) string {
	ext := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	if !extPattern.MatchString(ext) {
		return ".bin"
	}
	return ext
}
