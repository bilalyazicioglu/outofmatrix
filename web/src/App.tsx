import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  ArrowUpDownIcon,
  HeartIcon,
  ImageIcon,
  Loader2Icon,
  LogOutIcon,
  MusicIcon,
  PlayIcon,
  SearchIcon,
  SparklesIcon,
  UserIcon,
  VideoIcon,
  XIcon,
} from "lucide-react"
import { toast } from "sonner"

import {
  clearSession,
  createCollection,
  deleteCollection,
  getCollection,
  getMedia,
  getStoredUser,
  getToken,
  listCollections,
  listMedia,
  patchMedia,
  removeFromCollection,
  uploadFile,
  type Collection,
  type MediaEvent,
  type MediaItem,
  type MediaSort,
  type MediaType,
  type User,
} from "@/lib/api"
import { useMediaEvents } from "@/hooks/use-media-events"
import { cn } from "@/lib/utils"
import { AlbumsBar } from "@/components/albums-bar"
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
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
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

const SORT_LABELS: Record<MediaSort, string> = {
  added: "Date added",
  captured: "Date taken",
  name: "Name",
}

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
  const [favOnly, setFavOnly] = useState(false)
  const [searchInput, setSearchInput] = useState("")
  const [query, setQuery] = useState("")
  const [sort, setSort] = useState<MediaSort>("added")
  const [ascending, setAscending] = useState(false)
  const [albums, setAlbums] = useState<Collection[]>([])
  const [activeAlbum, setActiveAlbum] = useState<Collection | null>(null)
  const [albumItems, setAlbumItems] = useState<MediaItem[]>([])
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [live, setLive] = useState<Record<string, MediaEvent>>({})
  const [uploads, setUploads] = useState<ActiveUpload[]>([])
  const [playing, setPlaying] = useState<MediaItem | null>(null)

  // Latest view state for use inside long-lived callbacks (WS, uploads).
  const viewRef = useRef({ filter, favOnly, query, activeAlbum })
  viewRef.current = { filter, favOnly, query, activeAlbum }

  // Debounce the search box into the applied query.
  useEffect(() => {
    const t = setTimeout(() => setQuery(searchInput.trim()), 300)
    return () => clearTimeout(t)
  }, [searchInput])

  // --- Data loading ------------------------------------------------------------

  const load = useCallback(async () => {
    setLoading(true)
    try {
      if (viewRef.current.activeAlbum) {
        const res = await getCollection(viewRef.current.activeAlbum.id)
        setAlbumItems(res.items)
      } else {
        const res = await listMedia({
          type: viewRef.current.filter,
          favorite: viewRef.current.favOnly,
          query: viewRef.current.query,
          sort,
          ascending,
          limit: PAGE_SIZE,
          offset: 0,
        })
        setItems(res.items)
        setTotal(res.total)
      }
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load library")
    } finally {
      setLoading(false)
    }
  }, [sort, ascending])

  useEffect(() => {
    void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter, favOnly, query, sort, ascending, activeAlbum])

  useEffect(() => {
    listCollections()
      .then(setAlbums)
      .catch(() => toast.error("Failed to load albums"))
  }, [])

  const loadMore = async () => {
    setLoadingMore(true)
    try {
      const res = await listMedia({
        type: filter,
        favorite: favOnly,
        query,
        sort,
        ascending,
        limit: PAGE_SIZE,
        offset: items.length,
      })
      setItems((prev) => [...prev, ...res.items])
      setTotal(res.total)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to load more")
    } finally {
      setLoadingMore(false)
    }
  }

  // In album mode, filters and sorting apply client-side to the album's items;
  // albums are curated and small, so this stays cheap.
  const displayed = useMemo(() => {
    if (!activeAlbum) return items
    let list = albumItems
    if (filter) list = list.filter((i) => i.type === filter)
    if (favOnly) list = list.filter((i) => i.is_favorite)
    if (query) {
      const q = query.toLowerCase()
      list = list.filter((i) => i.title.toLowerCase().includes(q))
    }
    if (sort === "added" && !ascending) return list // curated album order
    const dir = ascending ? 1 : -1
    return [...list].sort((a, b) => {
      switch (sort) {
        case "name":
          return dir * a.title.localeCompare(b.title, undefined, { sensitivity: "base" })
        case "captured": {
          const ta = Date.parse(a.captured_at ?? a.created_at)
          const tb = Date.parse(b.captured_at ?? b.created_at)
          return dir * (ta - tb)
        }
        default:
          return dir * (Date.parse(a.created_at) - Date.parse(b.created_at))
      }
    })
  }, [activeAlbum, items, albumItems, filter, favOnly, query, sort, ascending])

  const shownTotal = activeAlbum ? displayed.length : total

  // --- Item mutations ------------------------------------------------------------

  const replaceItem = useCallback((updated: MediaItem) => {
    const map = (prev: MediaItem[]) =>
      prev.map((i) => (i.id === updated.id ? updated : i))
    setItems(map)
    setAlbumItems(map)
    setPlaying((p) => (p && p.id === updated.id ? updated : p))
  }, [])

  const removeItem = useCallback((id: string) => {
    setItems((prev) => prev.filter((i) => i.id !== id))
    setAlbumItems((prev) => prev.filter((i) => i.id !== id))
    setTotal((t) => Math.max(0, t - 1))
  }, [])

  const toggleFavorite = useCallback(
    (item: MediaItem) => {
      const next = !item.is_favorite
      // Optimistic flip; revert on failure.
      replaceItem({ ...item, is_favorite: next })
      patchMedia(item.id, { is_favorite: next })
        .then((updated) => {
          replaceItem(updated)
          // The item no longer belongs to a favorites-only server view.
          if (!next && viewRef.current.favOnly && !viewRef.current.activeAlbum) {
            setItems((prev) => prev.filter((i) => i.id !== updated.id))
            setTotal((t) => Math.max(0, t - 1))
          }
        })
        .catch((err: unknown) => {
          replaceItem(item)
          toast.error(err instanceof Error ? err.message : "Update failed")
        })
    },
    [replaceItem]
  )

  // --- Albums ------------------------------------------------------------------

  const createAlbum = useCallback(async (name: string) => {
    try {
      const album = await createCollection(name)
      setAlbums((prev) => [...prev, album])
      toast.success(`Album "${album.name}" created`)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to create album")
      throw err
    }
  }, [])

  const deleteAlbum = useCallback((album: Collection) => {
    deleteCollection(album.id)
      .then(() => {
        setAlbums((prev) => prev.filter((a) => a.id !== album.id))
        setActiveAlbum((a) => (a?.id === album.id ? null : a))
        toast.success(`Album "${album.name}" deleted`)
      })
      .catch((err: unknown) =>
        toast.error(err instanceof Error ? err.message : "Failed to delete album")
      )
  }, [])

  const removeFromAlbum = useCallback(
    (item: MediaItem) => {
      const album = viewRef.current.activeAlbum
      if (!album) return
      removeFromCollection(album.id, item.id)
        .then(() => {
          setAlbumItems((prev) => prev.filter((i) => i.id !== item.id))
          setPlaying(null)
          toast.success(`Removed from "${album.name}"`)
        })
        .catch((err: unknown) =>
          toast.error(err instanceof Error ? err.message : "Failed to remove")
        )
    },
    []
  )

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
          const v = viewRef.current
          if (!v.activeAlbum && (v.filter === "" || v.filter === item.type)) {
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

  // --- Player navigation ----------------------------------------------------------

  const playable = useMemo(
    () => displayed.filter((i) => i.status === "ready"),
    [displayed]
  )
  const playingIndex = playing
    ? playable.findIndex((i) => i.id === playing.id)
    : -1
  const onPrev =
    playingIndex > 0 ? () => setPlaying(playable[playingIndex - 1]) : undefined
  const onNext =
    playingIndex >= 0 && playingIndex < playable.length - 1
      ? () => setPlaying(playable[playingIndex + 1])
      : undefined

  // --- Render --------------------------------------------------------------------

  const directionLabels =
    sort === "name"
      ? { desc: "Z → A", asc: "A → Z" }
      : { desc: "Newest first", asc: "Oldest first" }

  const emptyMessage = activeAlbum
    ? "This album is empty — open something and use the Album button to file it here."
    : favOnly
      ? "No favorites yet — tap the heart on anything you love."
      : query
        ? `Nothing matches "${query}".`
        : "Drop some photos, music or videos above — they'll be transcoded for streaming automatically."

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

      <main className="mx-auto grid max-w-6xl gap-5 px-4 py-6">
        <UploadZone
          uploads={uploads}
          onFiles={startUploads}
          onDismiss={dismissUpload}
        />

        <AlbumsBar
          albums={albums}
          active={activeAlbum}
          onSelect={setActiveAlbum}
          onCreate={createAlbum}
          onDelete={deleteAlbum}
        />

        <div className="flex flex-wrap items-center gap-2">
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

          <Button
            variant={favOnly ? "secondary" : "ghost"}
            size="sm"
            aria-pressed={favOnly}
            onClick={() => setFavOnly((f) => !f)}
            className={cn(favOnly && "text-rose-300")}
          >
            <HeartIcon
              className={cn("size-4", favOnly && "fill-rose-500 text-rose-500")}
            />
            Favorites
          </Button>

          <div className="ml-auto flex items-center gap-2">
            <div className="relative">
              <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={searchInput}
                onChange={(e) => setSearchInput(e.target.value)}
                placeholder="Search…"
                aria-label="Search library"
                className="h-9 w-40 pl-8 sm:w-56"
              />
              {searchInput && (
                <button
                  type="button"
                  aria-label="Clear search"
                  onClick={() => setSearchInput("")}
                  className="absolute top-1/2 right-2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"
                >
                  <XIcon className="size-3.5" />
                </button>
              )}
            </div>

            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="h-9">
                  <ArrowUpDownIcon className="size-4" />
                  <span className="hidden sm:inline">{SORT_LABELS[sort]}</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuLabel>Sort by</DropdownMenuLabel>
                <DropdownMenuRadioGroup
                  value={sort}
                  onValueChange={(v) => setSort(v as MediaSort)}
                >
                  <DropdownMenuRadioItem value="added">
                    Date added
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="captured">
                    Date taken
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="name">
                    Name
                  </DropdownMenuRadioItem>
                </DropdownMenuRadioGroup>
                <DropdownMenuSeparator />
                <DropdownMenuRadioGroup
                  value={ascending ? "asc" : "desc"}
                  onValueChange={(v) => setAscending(v === "asc")}
                >
                  <DropdownMenuRadioItem value="desc">
                    {directionLabels.desc}
                  </DropdownMenuRadioItem>
                  <DropdownMenuRadioItem value="asc">
                    {directionLabels.asc}
                  </DropdownMenuRadioItem>
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>

            <span className="hidden text-xs whitespace-nowrap text-muted-foreground md:inline">
              {shownTotal} item{shownTotal === 1 ? "" : "s"}
            </span>
          </div>
        </div>

        {loading ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
            {Array.from({ length: 10 }, (_, i) => (
              <Skeleton key={i} className="aspect-square rounded-xl" />
            ))}
          </div>
        ) : displayed.length === 0 ? (
          <div className="flex flex-col items-center gap-3 rounded-2xl border border-white/[0.06] bg-white/[0.02] px-6 py-20 text-center">
            <div className="flex size-14 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-500/20 to-fuchsia-500/15">
              {favOnly ? (
                <HeartIcon className="size-7 text-rose-300/70" />
              ) : (
                <SparklesIcon className="size-7 text-violet-300/70" />
              )}
            </div>
            <p className="font-medium">
              {activeAlbum
                ? `"${activeAlbum.name}" is empty`
                : favOnly
                  ? "No favorites yet"
                  : query
                    ? "No results"
                    : "Your library is empty"}
            </p>
            <p className="max-w-sm text-sm text-muted-foreground">
              {emptyMessage}
            </p>
          </div>
        ) : (
          <>
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
              {displayed.map((item) => (
                <MediaCard
                  key={item.id}
                  item={item}
                  live={live[item.id]}
                  onOpen={setPlaying}
                  onToggleFavorite={toggleFavorite}
                />
              ))}
            </div>

            {!activeAlbum && items.length < total && (
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
        onDeleted={removeItem}
        onUpdated={replaceItem}
        onPrev={onPrev}
        onNext={onNext}
        albums={albums}
        activeAlbum={activeAlbum}
        onRemoveFromAlbum={removeFromAlbum}
      />
    </div>
  )
}
