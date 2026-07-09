import { useEffect, useState, type FormEvent } from "react"
import {
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  DownloadIcon,
  FolderPlusIcon,
  FolderMinusIcon,
  HeartIcon,
  Loader2Icon,
  MusicIcon,
  PencilIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"
import { toast } from "sonner"

import {
  addToCollection,
  deleteMedia,
  formatBytes,
  formatDuration,
  hlsMasterUrl,
  patchMedia,
  rawUrl,
  thumbUrl,
  type Collection,
  type MediaItem,
} from "@/lib/api"
import { cn } from "@/lib/utils"
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"

interface PlayerDialogProps {
  item: MediaItem | null
  onClose: () => void
  onDeleted: (id: string) => void
  /** Called with the fresh item after a rename or favorite toggle. */
  onUpdated: (item: MediaItem) => void
  onPrev?: () => void
  onNext?: () => void
  albums: Collection[]
  /** Set when the grid is scoped to an album: enables "remove from album". */
  activeAlbum?: Collection | null
  onRemoveFromAlbum?: (item: MediaItem) => void
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

export function PlayerDialog({
  item,
  onClose,
  onDeleted,
  onUpdated,
  onPrev,
  onNext,
  albums,
  activeAlbum,
  onRemoveFromAlbum,
}: PlayerDialogProps) {
  const { videoRef, loading } = useHls(item)
  const [deleting, setDeleting] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [renaming, setRenaming] = useState(false)
  const [draftTitle, setDraftTitle] = useState("")

  useEffect(() => {
    setConfirming(false)
    setRenaming(false)
  }, [item])

  const toggleFavorite = async () => {
    if (!item) return
    try {
      const updated = await patchMedia(item.id, {
        is_favorite: !item.is_favorite,
      })
      onUpdated(updated)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Update failed")
    }
  }

  const saveTitle = async (e: FormEvent) => {
    e.preventDefault()
    if (!item) return
    const trimmed = draftTitle.trim()
    if (!trimmed || trimmed === item.title) {
      setRenaming(false)
      return
    }
    try {
      const updated = await patchMedia(item.id, { title: trimmed })
      onUpdated(updated)
      setRenaming(false)
      toast.success("Renamed")
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Rename failed")
    }
  }

  const addToAlbum = async (album: Collection) => {
    if (!item) return
    try {
      await addToCollection(album.id, item.id)
      toast.success(`Added to "${album.name}"`)
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed"
      if (msg.includes("exists")) {
        toast.info(`Already in "${album.name}"`)
      } else {
        toast.error(msg)
      }
    }
  }

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
      <DialogContent
        className="max-w-4xl gap-0 overflow-hidden border-white/10 bg-[oklch(0.14_0.03_283)] p-0 sm:max-w-4xl"
        onKeyDown={(e) => {
          if (renaming) return
          if (e.key === "ArrowLeft" && onPrev) {
            e.preventDefault()
            onPrev()
          } else if (e.key === "ArrowRight" && onNext) {
            e.preventDefault()
            onNext()
          }
        }}
      >
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
                    className="max-h-[68vh] w-full bg-black"
                  />
                </>
              )}

              {item.type === "photo" && (
                // w-auto/h-auto + object-contain: never distorts aspect ratio
                <img
                  src={rawUrl(item.id)}
                  alt={item.title}
                  className="h-auto max-h-[68vh] w-auto max-w-full object-contain"
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

              {/* Prev / next overlay arrows */}
              {onPrev && (
                <button
                  type="button"
                  aria-label="Previous"
                  onClick={onPrev}
                  className="absolute top-1/2 left-2 -translate-y-1/2 rounded-full bg-black/50 p-2 text-white/70 backdrop-blur-sm transition hover:bg-black/75 hover:text-white"
                >
                  <ChevronLeftIcon className="size-5" />
                </button>
              )}
              {onNext && (
                <button
                  type="button"
                  aria-label="Next"
                  onClick={onNext}
                  className="absolute top-1/2 right-2 -translate-y-1/2 rounded-full bg-black/50 p-2 text-white/70 backdrop-blur-sm transition hover:bg-black/75 hover:text-white"
                >
                  <ChevronRightIcon className="size-5" />
                </button>
              )}
            </div>

            <DialogHeader className="px-6 pt-5 text-left">
              {renaming ? (
                <form onSubmit={saveTitle} className="flex items-center gap-2 pr-8">
                  <Input
                    autoFocus
                    value={draftTitle}
                    onChange={(e) => setDraftTitle(e.target.value)}
                    maxLength={512}
                    className="h-9"
                    onKeyDown={(e) => e.key === "Escape" && setRenaming(false)}
                  />
                  <Button type="submit" size="icon" className="size-9 shrink-0" aria-label="Save name">
                    <CheckIcon className="size-4" />
                  </Button>
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    className="size-9 shrink-0"
                    aria-label="Cancel rename"
                    onClick={() => setRenaming(false)}
                  >
                    <XIcon className="size-4" />
                  </Button>
                </form>
              ) : (
                <DialogTitle className="group/title flex items-center gap-2 pr-8 text-lg">
                  <span className="truncate">{item.title}</span>
                  <button
                    type="button"
                    aria-label="Rename"
                    onClick={() => {
                      setDraftTitle(item.title)
                      setRenaming(true)
                    }}
                    className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition group-hover/title:opacity-100 hover:text-foreground focus-visible:opacity-100"
                  >
                    <PencilIcon className="size-3.5" />
                  </button>
                </DialogTitle>
              )}
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
                {item.captured_at && (
                  <Badge variant="outline">
                    Taken {new Date(item.captured_at).toLocaleDateString()}
                  </Badge>
                )}
              </div>
            </DialogHeader>

            <Separator className="mt-5 bg-white/[0.06]" />

            <DialogFooter className="flex-row items-center justify-between gap-2 px-6 py-4 sm:justify-between">
              <span className="hidden text-xs text-muted-foreground sm:inline">
                Added {new Date(item.created_at).toLocaleDateString()}
              </span>

              <div className="flex items-center gap-1">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={toggleFavorite}
                  aria-pressed={item.is_favorite}
                >
                  <HeartIcon
                    className={cn(
                      "size-4",
                      item.is_favorite && "fill-rose-500 text-rose-500"
                    )}
                  />
                  <span className="hidden sm:inline">
                    {item.is_favorite ? "Favorited" : "Favorite"}
                  </span>
                </Button>

                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button variant="ghost" size="sm">
                      <FolderPlusIcon className="size-4" />
                      <span className="hidden sm:inline">Album</span>
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-52">
                    <DropdownMenuLabel>Add to album</DropdownMenuLabel>
                    {albums.length === 0 && (
                      <DropdownMenuItem disabled>
                        No albums yet — create one below the upload zone
                      </DropdownMenuItem>
                    )}
                    {albums.map((album) => (
                      <DropdownMenuItem
                        key={album.id}
                        onClick={() => addToAlbum(album)}
                      >
                        <FolderPlusIcon className="size-4" />
                        <span className="truncate">{album.name}</span>
                      </DropdownMenuItem>
                    ))}
                    {activeAlbum && onRemoveFromAlbum && (
                      <>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          variant="destructive"
                          onClick={() => onRemoveFromAlbum(item)}
                        >
                          <FolderMinusIcon className="size-4" />
                          <span className="truncate">
                            Remove from “{activeAlbum.name}”
                          </span>
                        </DropdownMenuItem>
                      </>
                    )}
                  </DropdownMenuContent>
                </DropdownMenu>

                <Button variant="ghost" size="sm" asChild>
                  <a href={rawUrl(item.id)} download={item.title}>
                    <DownloadIcon className="size-4" />
                    <span className="hidden sm:inline">Download</span>
                  </a>
                </Button>

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
              </div>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
