import { useCallback, useEffect, useRef, useState } from "react"
import {
  ImageIcon,
  Loader2Icon,
  LogOutIcon,
  MusicIcon,
  PlayIcon,
  SparklesIcon,
  UserIcon,
  VideoIcon,
} from "lucide-react"
import { toast } from "sonner"

import {
  clearSession,
  getMedia,
  getStoredUser,
  getToken,
  listMedia,
  uploadFile,
  type MediaEvent,
  type MediaItem,
  type MediaType,
  type User,
} from "@/lib/api"
import { useMediaEvents } from "@/hooks/use-media-events"
import { cn } from "@/lib/utils"
import { Aurora } from "@/components/aurora"
import { LoginView } from "@/components/login-view"
import { MediaCard } from "@/components/media-card"
import { PlayerDialog } from "@/components/player-dialog"
import { UploadZone, type ActiveUpload } from "@/components/upload-zone"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Toaster } from "@/components/ui/sonner"

const PAGE_SIZE = 60

type Filter = "" | MediaType

const FILTERS: Array<{ value: Filter; label: string; icon: typeof ImageIcon }> =
  [
    { value: "", label: "All", icon: SparklesIcon },
    { value: "photo", label: "Photos", icon: ImageIcon },
    { value: "video", label: "Videos", icon: VideoIcon },
    { value: "audio", label: "Music", icon: MusicIcon },
  ]

export default function App() {
  const [user, setUser] = useState<User | null>(() =>
    getToken() ? getStoredUser() : null
  )

  if (!user) {
    return (
      <>
        <LoginView onLogin={setUser} />
        <Toaster position="bottom-right" />
      </>
    )
  }

  return (
    <>
      <Library
        user={user}
        onLogout={() => {
          clearSession()
          setUser(null)
        }}
      />
      <Toaster position="bottom-right" />
    </>
  )
}

function Library({ user, onLogout }: { user: User; onLogout: () => void }) {
  const [items, setItems] = useState<MediaItem[]>([])
  const [total, setTotal] = useState(0)
  const [filter, setFilter] = useState<Filter>("")
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [live, setLive] = useState<Record<string, MediaEvent>>({})
  const [uploads, setUploads] = useState<ActiveUpload[]>([])
  const [playing, setPlaying] = useState<MediaItem | null>(null)
  const filterRef = useRef(filter)
  filterRef.current = filter

  const refresh = useCallback(async (f: Filter) => {
    setLoading(true)
    try {
      const res = await listMedia(f, PAGE_SIZE, 0)
      setItems(res.items)
      setTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load library")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh(filter)
  }, [filter, refresh])

  const loadMore = async () => {
    setLoadingMore(true)
    try {
      const res = await listMedia(filter, PAGE_SIZE, items.length)
      setItems((prev) => [...prev, ...res.items])
      setTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load more")
    } finally {
      setLoadingMore(false)
    }
  }

  // --- Live processing events ------------------------------------------------

  const onEvent = useCallback((evt: MediaEvent) => {
    setLive((prev) => ({ ...prev, [evt.media_id]: evt }))

    if (evt.status === "completed") {
      // Pull the finished item (thumbnail, metadata, HLS path) and swap it in.
      getMedia(evt.media_id)
        .then((item) => {
          setItems((prev) => {
            const idx = prev.findIndex((i) => i.id === item.id)
            if (idx === -1) return prev
            const next = [...prev]
            next[idx] = item
            return next
          })
          setLive((prev) => {
            const next = { ...prev }
            delete next[evt.media_id]
            return next
          })
          toast.success(`"${item.title}" is ready`)
        })
        .catch(() => {
          // Item may be filtered out of the current view; drop the event.
          setLive((prev) => {
            const next = { ...prev }
            delete next[evt.media_id]
            return next
          })
        })
    } else if (evt.status === "failed") {
      setItems((prev) =>
        prev.map((i) =>
          i.id === evt.media_id ? { ...i, status: "failed" as const } : i
        )
      )
      toast.error(
        `Processing failed${evt.title ? `: ${evt.title}` : ""}`,
        evt.error ? { description: evt.error } : undefined
      )
    }
  }, [])

  const connected = useMediaEvents(true, onEvent)

  // --- Uploads -----------------------------------------------------------------

  const startUploads = useCallback((files: File[]) => {
    for (const file of files) {
      const key = crypto.randomUUID()
      const controller = new AbortController()
      let lastPaint = 0

      setUploads((prev) => [
        ...prev,
        {
          key,
          name: file.name,
          size: file.size,
          sentBytes: 0,
          percent: 0,
          abort: () => controller.abort(),
        },
      ])

      uploadFile(
        file,
        (p) => {
          // XHR progress fires very frequently; repaint at most ~8×/second.
          const now = performance.now()
          if (p.percent < 100 && now - lastPaint < 120) return
          lastPaint = now
          setUploads((prev) =>
            prev.map((u) =>
              u.key === key
                ? { ...u, sentBytes: p.sentBytes, percent: p.percent }
                : u
            )
          )
        },
        controller.signal
      )
        .then((item) => {
          setUploads((prev) => prev.filter((u) => u.key !== key))
          setLive((prev) => ({
            ...prev,
            [item.id]: {
              media_id: item.id,
              status: "queued",
              progress: 0,
            },
          }))
          const f = filterRef.current
          if (f === "" || f === item.type) {
            setItems((prev) =>
              prev.some((i) => i.id === item.id) ? prev : [item, ...prev]
            )
            setTotal((t) => t + 1)
          }
        })
        .catch((err: unknown) => {
          if (err instanceof DOMException && err.name === "AbortError") {
            setUploads((prev) => prev.filter((u) => u.key !== key))
            return
          }
          const msg = err instanceof Error ? err.message : "upload failed"
          setUploads((prev) =>
            prev.map((u) => (u.key === key ? { ...u, error: msg } : u))
          )
        })
    }
  }, [])

  const dismissUpload = useCallback((key: string) => {
    setUploads((prev) => prev.filter((u) => u.key !== key))
  }, [])

  // --- Render --------------------------------------------------------------------

  return (
    <div className="relative min-h-svh">
      <Aurora />

      <header className="sticky top-0 z-40 border-b border-white/[0.06] bg-background/70 backdrop-blur-xl">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-4 px-4">
          <div className="flex items-center gap-2.5">
            <div className="flex size-8 items-center justify-center rounded-lg bg-gradient-to-br from-violet-500 to-fuchsia-500 shadow-md shadow-violet-500/25">
              <PlayIcon className="size-4 fill-white text-white" />
            </div>
            <span className="text-gradient text-lg font-semibold tracking-tight">
              outofmatrix
            </span>
          </div>

          <div
            className="ml-auto flex items-center gap-2 text-xs text-muted-foreground"
            title={connected ? "Live updates connected" : "Reconnecting…"}
          >
            <span className="relative flex size-2">
              {connected && (
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60" />
              )}
              <span
                className={cn(
                  "relative inline-flex size-2 rounded-full",
                  connected ? "bg-emerald-400" : "bg-zinc-500"
                )}
              />
            </span>
            <span className="hidden sm:inline">
              {connected ? "Live" : "Offline"}
            </span>
          </div>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" className="gap-2">
                <span className="flex size-6 items-center justify-center rounded-full bg-gradient-to-br from-violet-500/40 to-fuchsia-500/30 text-xs font-semibold uppercase">
                  {user.username.slice(0, 1)}
                </span>
                <span className="hidden max-w-28 truncate text-sm sm:inline">
                  {user.username}
                </span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuLabel className="flex items-center gap-2">
                <UserIcon className="size-4 text-muted-foreground" />
                <span className="truncate">{user.username}</span>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={onLogout} variant="destructive">
                <LogOutIcon className="size-4" />
                Sign out
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>

      <main className="mx-auto grid max-w-6xl gap-6 px-4 py-6">
        <UploadZone
          uploads={uploads}
          onFiles={startUploads}
          onDismiss={dismissUpload}
        />

        <div className="flex flex-wrap items-center justify-between gap-3">
          <Tabs value={filter} onValueChange={(v) => setFilter(v as Filter)}>
            <TabsList>
              {FILTERS.map(({ value, label, icon: Icon }) => (
                <TabsTrigger key={value || "all"} value={value}>
                  <Icon className="size-3.5" />
                  {label}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          <span className="text-xs text-muted-foreground">
            {total} item{total === 1 ? "" : "s"}
          </span>
        </div>

        {loading ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {Array.from({ length: 10 }, (_, i) => (
              <Skeleton key={i} className="aspect-square rounded-xl" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center gap-3 rounded-2xl border border-white/[0.06] bg-white/[0.02] px-6 py-20 text-center">
            <div className="flex size-14 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-500/20 to-fuchsia-500/15">
              <SparklesIcon className="size-7 text-violet-300/70" />
            </div>
            <p className="font-medium">Your library is empty</p>
            <p className="max-w-sm text-sm text-muted-foreground">
              Drop some photos, music or videos above — they'll be transcoded
              for streaming automatically.
            </p>
          </div>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {items.map((item) => (
                <MediaCard
                  key={item.id}
                  item={item}
                  live={live[item.id]}
                  onOpen={setPlaying}
                />
              ))}
            </div>

            {items.length < total && (
              <div className="flex justify-center pb-4">
                <Button
                  variant="secondary"
                  onClick={loadMore}
                  disabled={loadingMore}
                >
                  {loadingMore && (
                    <Loader2Icon className="size-4 animate-spin" />
                  )}
                  Load more ({total - items.length} left)
                </Button>
              </div>
            )}
          </>
        )}
      </main>

      <PlayerDialog
        item={playing}
        onClose={() => setPlaying(null)}
        onDeleted={(id) => {
          setItems((prev) => prev.filter((i) => i.id !== id))
          setTotal((t) => Math.max(0, t - 1))
        }}
      />
    </div>
  )
}
