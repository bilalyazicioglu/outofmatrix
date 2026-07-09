import { useState, type FormEvent } from "react"
import {
  FolderIcon,
  FolderOpenIcon,
  Loader2Icon,
  PlusIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react"

import type { Collection } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

interface AlbumsBarProps {
  albums: Collection[]
  active: Collection | null
  onSelect: (album: Collection | null) => void
  onCreate: (name: string) => Promise<void>
  onDelete: (album: Collection) => void
}

/**
 * Horizontal strip of album chips: the user's filing system. Selecting a chip
 * scopes the grid to that album; the active chip grows a delete control.
 */
export function AlbumsBar({
  albums,
  active,
  onSelect,
  onCreate,
  onDelete,
}: AlbumsBarProps) {
  const [creating, setCreating] = useState(false)
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed || busy) return
    setBusy(true)
    try {
      await onCreate(trimmed)
      setName("")
      setCreating(false)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="mr-1 text-xs font-medium tracking-wide text-muted-foreground uppercase">
        Albums
      </span>

      {albums.map((album) => {
        const isActive = active?.id === album.id
        return (
          <span key={album.id} className="inline-flex items-center">
            <button
              type="button"
              onClick={() => {
                setConfirmDelete(false)
                onSelect(isActive ? null : album)
              }}
              className={cn(
                "inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-sm",
                "transition-all duration-200",
                isActive
                  ? "border-violet-400/50 bg-violet-500/20 text-violet-100 shadow-sm shadow-violet-500/20"
                  : "border-white/10 bg-white/[0.03] text-muted-foreground hover:border-white/20 hover:bg-white/[0.06] hover:text-foreground"
              )}
            >
              {isActive ? (
                <FolderOpenIcon className="size-3.5" />
              ) : (
                <FolderIcon className="size-3.5" />
              )}
              {album.name}
            </button>

            {isActive && (
              <button
                type="button"
                aria-label={
                  confirmDelete ? "Confirm delete album" : "Delete album"
                }
                onClick={() => {
                  if (confirmDelete) {
                    setConfirmDelete(false)
                    onDelete(album)
                  } else {
                    setConfirmDelete(true)
                  }
                }}
                onBlur={() => setConfirmDelete(false)}
                className={cn(
                  "ml-1 flex h-8 items-center gap-1 rounded-full border px-2 text-xs",
                  "transition-colors",
                  confirmDelete
                    ? "border-red-400/50 bg-red-500/20 text-red-200"
                    : "border-white/10 bg-white/[0.03] text-muted-foreground hover:text-red-300"
                )}
              >
                <Trash2Icon className="size-3.5" />
                {confirmDelete && "Sure?"}
              </button>
            )}
          </span>
        )
      })}

      {creating ? (
        <form onSubmit={submit} className="inline-flex items-center gap-1.5">
          <Input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Album name"
            maxLength={128}
            className="h-8 w-40"
          />
          <Button type="submit" size="sm" className="h-8" disabled={busy}>
            {busy ? (
              <Loader2Icon className="size-3.5 animate-spin" />
            ) : (
              "Create"
            )}
          </Button>
          <Button
            type="button"
            size="icon"
            variant="ghost"
            className="size-8"
            aria-label="Cancel"
            onClick={() => {
              setCreating(false)
              setName("")
            }}
          >
            <XIcon className="size-4" />
          </Button>
        </form>
      ) : (
        <button
          type="button"
          onClick={() => setCreating(true)}
          className="inline-flex h-8 items-center gap-1 rounded-full border border-dashed border-white/15 px-3 text-sm text-muted-foreground transition-colors hover:border-violet-400/40 hover:text-violet-200"
        >
          <PlusIcon className="size-3.5" />
          New album
        </button>
      )}
    </div>
  )
}
