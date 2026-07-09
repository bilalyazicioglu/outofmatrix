import { memo, useMemo, useState } from "react"
import {
  HeartIcon,
  ImageIcon,
  Loader2Icon,
  MusicIcon,
  PlayIcon,
  TriangleAlertIcon,
} from "lucide-react"

import {
  formatDuration,
  thumbUrl,
  type MediaEvent,
  type MediaItem,
} from "@/lib/api"
import { blurHashToDataURL } from "@/lib/blurhash"
import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"

interface MediaCardProps {
  item: MediaItem
  /** Latest WebSocket event for this item while it is being processed. */
  live?: MediaEvent
  onOpen: (item: MediaItem) => void
  onToggleFavorite: (item: MediaItem) => void
}

const typeIcon = {
  photo: ImageIcon,
  video: PlayIcon,
  audio: MusicIcon,
} as const

function stageLabel(evt?: MediaEvent): string {
  if (!evt) return "Queued"
  switch (evt.stage) {
    case "probe":
      return "Analyzing"
    case "thumbnail":
      return "Thumbnail"
    case "transcode":
      return "Transcoding"
    default:
      return evt.status === "queued" ? "Queued" : "Processing"
  }
}

export const MediaCard = memo(function MediaCard({
  item,
  live,
  onOpen,
  onToggleFavorite,
}: MediaCardProps) {
  const [thumbLoaded, setThumbLoaded] = useState(false)

  const placeholder = useMemo(
    () => (item.blur_hash ? blurHashToDataURL(item.blur_hash) : null),
    [item.blur_hash]
  )

  const processing = item.status === "pending" || item.status === "processing"
  const failed = item.status === "failed" || live?.status === "failed"
  const ready = item.status === "ready"
  const hasThumb = ready && !!item.thumbnail_path
  const Icon = typeIcon[item.type]
  const duration =
    item.type === "photo" ? "" : formatDuration(item.metadata?.duration_seconds)
  const progress = live ? Math.max(0, Math.min(100, live.progress)) : 0

  return (
    <div
      role="button"
      tabIndex={ready ? 0 : -1}
      aria-label={item.title}
      onClick={() => ready && onOpen(item)}
      onKeyDown={(e) => {
        if (ready && (e.key === "Enter" || e.key === " ")) {
          e.preventDefault()
          onOpen(item)
        }
      }}
      className={cn(
        "media-cell group relative aspect-square w-full overflow-hidden rounded-xl",
        "border border-white/[0.06] bg-secondary/40 text-left",
        "transition-all duration-300 ease-out",
        ready &&
          "cursor-pointer hover:-translate-y-1 hover:border-white/15 hover:shadow-xl hover:shadow-violet-950/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      )}
    >
      {/* BlurHash placeholder — shown instantly, sits behind the real thumb */}
      {placeholder && (
        <img
          src={placeholder}
          alt=""
          aria-hidden
          className="absolute inset-0 h-full w-full scale-110 object-cover"
        />
      )}

      {hasThumb ? (
        <img
          src={thumbUrl(item.id)}
          alt={item.title}
          loading="lazy"
          decoding="async"
          onLoad={() => setThumbLoaded(true)}
          className={cn(
            "absolute inset-0 h-full w-full object-cover transition-[opacity,transform] duration-500",
            thumbLoaded ? "opacity-100" : "opacity-0",
            "group-hover:scale-[1.04]"
          )}
        />
      ) : (
        !placeholder && (
          <div className="absolute inset-0 flex items-center justify-center bg-gradient-to-br from-violet-950/60 via-secondary to-fuchsia-950/40">
            <Icon className="size-10 text-white/20" />
          </div>
        )
      )}

      {/* Bottom gradient with title */}
      <div className="absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/40 to-transparent p-3 pt-10">
        <p className="truncate text-sm font-medium text-white/95">
          {item.title}
        </p>
        <div className="mt-1 flex items-center gap-1.5 text-[11px] text-white/60">
          <Icon className="size-3" />
          <span className="capitalize">{item.type}</span>
          {duration && (
            <>
              <span aria-hidden>·</span>
              <span>{duration}</span>
            </>
          )}
        </div>
      </div>

      {/* Favorite toggle: always visible when favorited, on hover otherwise */}
      {ready && (
        <button
          type="button"
          aria-label={item.is_favorite ? "Remove from favorites" : "Add to favorites"}
          aria-pressed={item.is_favorite}
          onClick={(e) => {
            e.stopPropagation()
            onToggleFavorite(item)
          }}
          className={cn(
            "absolute top-2 left-2 z-10 flex size-7 items-center justify-center rounded-full",
            "bg-black/50 backdrop-blur-sm transition-all duration-200",
            "hover:scale-110 hover:bg-black/70 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none",
            item.is_favorite
              ? "opacity-100"
              : "opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
          )}
        >
          <HeartIcon
            className={cn(
              "size-4 transition-colors",
              item.is_favorite ? "fill-rose-500 text-rose-500" : "text-white/80"
            )}
          />
        </button>
      )}

      {/* Type chip */}
      <Badge
        variant="secondary"
        className="absolute top-2 right-2 border-white/10 bg-black/50 text-white/80 backdrop-blur-sm"
      >
        <Icon className="size-3" />
      </Badge>

      {/* Processing veil with live progress */}
      {processing && !failed && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-black/70 backdrop-blur-[2px]">
          <Loader2Icon className="size-6 animate-spin text-violet-300" />
          <div className="flex flex-col items-center gap-1.5">
            <span className="text-xs font-medium text-white/90">
              {stageLabel(live)}
              {live && live.progress > 0 && ` ${Math.round(progress)}%`}
            </span>
            <div className="h-1 w-28 overflow-hidden rounded-full bg-white/15">
              <div
                className="shimmer h-full rounded-full bg-gradient-to-r from-violet-400 to-fuchsia-400 transition-[width] duration-300"
                style={{ width: `${Math.max(progress, 4)}%` }}
              />
            </div>
          </div>
        </div>
      )}

      {failed && (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-black/75">
          <TriangleAlertIcon className="size-6 text-red-400" />
          <span className="px-4 text-center text-xs text-red-300">
            Processing failed
          </span>
        </div>
      )}
    </div>
  )
})
