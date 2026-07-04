import { useCallback, useRef, useState, type DragEvent } from "react"
import { CloudUploadIcon, FileIcon, Loader2Icon, XIcon } from "lucide-react"

import { formatBytes } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"

export interface ActiveUpload {
  key: string
  name: string
  size: number
  sentBytes: number
  percent: number
  error?: string
  abort: () => void
}

interface UploadZoneProps {
  uploads: ActiveUpload[]
  onFiles: (files: File[]) => void
  onDismiss: (key: string) => void
}

export function UploadZone({ uploads, onFiles, onDismiss }: UploadZoneProps) {
  const [dragging, setDragging] = useState(false)
  const inputRef = useRef<HTMLInputElement | null>(null)
  const dragDepth = useRef(0)

  const handleDrop = useCallback(
    (e: DragEvent) => {
      e.preventDefault()
      dragDepth.current = 0
      setDragging(false)
      const files = Array.from(e.dataTransfer.files)
      if (files.length) onFiles(files)
    },
    [onFiles]
  )

  return (
    <section className="grid gap-3">
      <div
        role="button"
        tabIndex={0}
        aria-label="Upload media"
        onClick={() => inputRef.current?.click()}
        onKeyDown={(e) => e.key === "Enter" && inputRef.current?.click()}
        onDragEnter={(e) => {
          e.preventDefault()
          dragDepth.current++
          setDragging(true)
        }}
        onDragLeave={(e) => {
          e.preventDefault()
          if (--dragDepth.current <= 0) {
            dragDepth.current = 0
            setDragging(false)
          }
        }}
        onDragOver={(e) => e.preventDefault()}
        onDrop={handleDrop}
        className={cn(
          "group relative flex cursor-pointer flex-col items-center justify-center gap-2",
          "rounded-2xl border border-dashed px-6 py-10 text-center",
          "transition-all duration-300",
          dragging
            ? "scale-[1.01] border-violet-400/70 bg-violet-500/10 shadow-lg shadow-violet-500/10"
            : "border-white/15 bg-white/[0.02] hover:border-violet-400/40 hover:bg-white/[0.04]"
        )}
      >
        <div
          className={cn(
            "flex size-12 items-center justify-center rounded-xl transition-all duration-300",
            "bg-gradient-to-br from-violet-500/25 to-fuchsia-500/20",
            dragging
              ? "scale-110 from-violet-500/50 to-fuchsia-500/40"
              : "group-hover:scale-105"
          )}
        >
          <CloudUploadIcon
            className={cn(
              "size-6 transition-colors",
              dragging ? "text-violet-200" : "text-violet-300/80"
            )}
          />
        </div>
        <p className="text-sm font-medium">
          {dragging ? "Drop to upload" : "Drag & drop photos, music or video"}
        </p>
        <p className="text-xs text-muted-foreground">
          or click to browse — large files upload in resumable chunks
        </p>
        <input
          ref={inputRef}
          type="file"
          multiple
          accept="image/*,video/*,audio/*"
          className="hidden"
          onChange={(e) => {
            const files = Array.from(e.target.files ?? [])
            if (files.length) onFiles(files)
            e.target.value = ""
          }}
        />
      </div>

      {uploads.length > 0 && (
        <div className="grid gap-2">
          {uploads.map((u) => (
            <div
              key={u.key}
              className="animate-fade-up flex items-center gap-3 rounded-xl border border-white/[0.06] bg-card/60 px-4 py-3 backdrop-blur-sm"
            >
              <div className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-violet-500/15">
                {u.error ? (
                  <FileIcon className="size-4 text-red-400" />
                ) : u.percent >= 100 ? (
                  <Loader2Icon className="size-4 animate-spin text-violet-300" />
                ) : (
                  <FileIcon className="size-4 text-violet-300" />
                )}
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex items-baseline justify-between gap-3">
                  <p className="truncate text-sm font-medium">{u.name}</p>
                  <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                    {u.error
                      ? "failed"
                      : u.percent >= 100
                        ? "finalizing…"
                        : `${formatBytes(u.sentBytes)} / ${formatBytes(u.size)}`}
                  </span>
                </div>
                {u.error ? (
                  <p className="mt-1 truncate text-xs text-red-400">{u.error}</p>
                ) : (
                  <Progress value={u.percent} className="mt-2 h-1.5" />
                )}
              </div>
              <Button
                variant="ghost"
                size="icon"
                className="size-7 shrink-0"
                aria-label={u.error ? "Dismiss" : "Cancel upload"}
                onClick={() => {
                  u.abort()
                  onDismiss(u.key)
                }}
              >
                <XIcon className="size-4" />
              </Button>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
