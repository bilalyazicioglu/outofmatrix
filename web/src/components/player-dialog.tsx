import { useEffect, useState } from "react"
import { Loader2Icon, MusicIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import {
  deleteMedia,
  formatBytes,
  formatDuration,
  hlsMasterUrl,
  rawUrl,
  thumbUrl,
  type MediaItem,
} from "@/lib/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Separator } from "@/components/ui/separator"

interface PlayerDialogProps {
  item: MediaItem | null
  onClose: () => void
  onDeleted: (id: string) => void
}

/**
 * Attaches an adaptive HLS stream to the <video>. hls.js is dynamically
 * imported so its chunk never loads until the first video is actually played;
 * where MSE is unavailable (e.g. iOS Safari) native HLS playback is used.
 *
 * The video element is tracked with a callback ref into state: Radix mounts
 * dialog content in a portal a tick after `item` changes, so an effect keyed
 * on the element itself is the only reliable attach point.
 */
function useHls(item: MediaItem | null) {
  const [videoEl, setVideoEl] = useState<HTMLVideoElement | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    const video = videoEl
    if (!video || !item || item.type !== "video") return

    const src = hlsMasterUrl(item.id)
    let destroyed = false
    let hls: { destroy: () => void } | null = null

    setLoading(true)
    import("hls.js").then(({ default: Hls }) => {
      if (destroyed) return
      setLoading(false)
      if (Hls.isSupported()) {
        const instance = new Hls({
          maxBufferLength: 30,
          backBufferLength: 30,
        })
        instance.loadSource(src)
        instance.attachMedia(video)
        instance.on(Hls.Events.ERROR, (_evt, data) => {
          if (data.fatal) {
            toast.error(`Playback error: ${data.type}`)
            instance.destroy()
          }
        })
        hls = instance
      } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
        video.src = src
      } else {
        toast.error("This browser cannot play HLS video")
      }
    })

    return () => {
      destroyed = true
      hls?.destroy()
      video.removeAttribute("src")
      video.load()
    }
  }, [videoEl, item])

  return { videoRef: setVideoEl, loading }
}

export function PlayerDialog({ item, onClose, onDeleted }: PlayerDialogProps) {
  const { videoRef, loading } = useHls(item)
  const [deleting, setDeleting] = useState(false)
  const [confirming, setConfirming] = useState(false)

  useEffect(() => setConfirming(false), [item])

  const handleDelete = async () => {
    if (!item) return
    if (!confirming) {
      setConfirming(true)
      return
    }
    setDeleting(true)
    try {
      await deleteMedia(item.id)
      toast.success(`Deleted "${item.title}"`)
      onDeleted(item.id)
      onClose()
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Delete failed")
    } finally {
      setDeleting(false)
    }
  }

  const meta = item?.metadata
  return (
    <Dialog open={!!item} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-4xl gap-0 overflow-hidden border-white/10 bg-[oklch(0.14_0.03_283)] p-0 sm:max-w-4xl">
        {item && (
          <>
            <div className="relative flex min-h-48 items-center justify-center bg-black">
              {item.type === "video" && (
                <>
                  {loading && (
                    <Loader2Icon className="absolute size-8 animate-spin text-violet-300" />
                  )}
                  <video
                    ref={videoRef}
                    controls
                    autoPlay
                    playsInline
                    poster={item.thumbnail_path ? thumbUrl(item.id) : undefined}
                    className="max-h-[70vh] w-full bg-black"
                  />
                </>
              )}

              {item.type === "photo" && (
                // w-auto/h-auto + object-contain: never distorts aspect ratio
                <img
                  src={rawUrl(item.id)}
                  alt={item.title}
                  className="h-auto max-h-[70vh] w-auto max-w-full object-contain"
                />
              )}

              {item.type === "audio" && (
                <div className="flex w-full flex-col items-center gap-6 px-8 py-12">
                  {item.thumbnail_path ? (
                    <img
                      src={thumbUrl(item.id)}
                      alt=""
                      className="size-44 rounded-2xl object-cover shadow-2xl shadow-violet-950/50"
                    />
                  ) : (
                    <div className="flex size-44 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-600/40 to-fuchsia-600/30">
                      <MusicIcon className="size-16 text-white/40" />
                    </div>
                  )}
                  <audio
                    src={rawUrl(item.id)}
                    controls
                    autoPlay
                    className="w-full max-w-md"
                  />
                </div>
              )}
            </div>

            <DialogHeader className="px-6 pt-5 text-left">
              <DialogTitle className="pr-8 text-lg">{item.title}</DialogTitle>
              <DialogDescription className="sr-only">
                Media details and playback
              </DialogDescription>
              <div className="flex flex-wrap items-center gap-2 pt-1">
                <Badge variant="secondary" className="capitalize">
                  {item.type}
                </Badge>
                {meta?.width && meta?.height ? (
                  <Badge variant="outline">
                    {meta.width}×{meta.height}
                  </Badge>
                ) : null}
                {meta?.video_codec && (
                  <Badge variant="outline" className="uppercase">
                    {meta.video_codec}
                  </Badge>
                )}
                {meta?.audio_codec && (
                  <Badge variant="outline" className="uppercase">
                    {meta.audio_codec}
                  </Badge>
                )}
                {item.type !== "photo" && meta?.duration_seconds ? (
                  <Badge variant="outline">
                    {formatDuration(meta.duration_seconds)}
                  </Badge>
                ) : null}
                <Badge variant="outline">{formatBytes(item.file_size)}</Badge>
              </div>
            </DialogHeader>

            <Separator className="mt-5 bg-white/[0.06]" />

            <DialogFooter className="flex-row items-center justify-between px-6 py-4 sm:justify-between">
              <span className="text-xs text-muted-foreground">
                Added {new Date(item.created_at).toLocaleDateString()}
              </span>
              <Button
                variant={confirming ? "destructive" : "ghost"}
                size="sm"
                disabled={deleting}
                onClick={handleDelete}
                onBlur={() => setConfirming(false)}
              >
                {deleting ? (
                  <Loader2Icon className="size-4 animate-spin" />
                ) : (
                  <Trash2Icon className="size-4" />
                )}
                {confirming ? "Really delete?" : "Delete"}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
