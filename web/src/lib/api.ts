// Typed client for the outofmatrix Go backend.

export type MediaType = "photo" | "video" | "audio"
export type MediaStatus = "pending" | "processing" | "ready" | "failed"

export interface MediaMetadata {
  duration_seconds?: number
  width?: number
  height?: number
  video_codec?: string
  audio_codec?: string
  bitrate_bps?: number
  sample_rate?: number
  channels?: number
  frame_rate?: number
  tags?: Record<string, string>
  processing_error?: string
}

export interface MediaItem {
  id: string
  user_id: string
  title: string
  type: MediaType
  status: MediaStatus
  file_size: number
  mime_type: string
  blur_hash?: string
  thumbnail_path?: string
  hls_path?: string
  metadata: MediaMetadata
  created_at: string
  updated_at: string
}

export interface User {
  id: string
  username: string
  role: string
  created_at: string
}

export interface MediaEvent {
  media_id: string
  title?: string
  type?: MediaType
  status: "queued" | "processing" | "completed" | "failed"
  stage?: "probe" | "thumbnail" | "transcode"
  progress: number
  error?: string
}

export interface MediaList {
  items: MediaItem[]
  total: number
  limit: number
  offset: number
}

interface UploadSession {
  id: string
  total_size: number
  chunk_size: number
  total_chunks: number
  received_chunks: number[]
}

const BASE = "/api/v1"
const TOKEN_KEY = "oom_token"
const USER_KEY = "oom_user"

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function getStoredUser(): User | null {
  const raw = localStorage.getItem(USER_KEY)
  if (!raw) return null
  try {
    return JSON.parse(raw) as User
  } catch {
    return null
  }
}

export function storeSession(token: string, user: User): void {
  localStorage.setItem(TOKEN_KEY, token)
  localStorage.setItem(USER_KEY, JSON.stringify(user))
}

export function clearSession(): void {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
}

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers)
  const token = getToken()
  if (token) headers.set("Authorization", `Bearer ${token}`)
  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json")
  }

  const res = await fetch(BASE + path, { ...init, headers })
  if (res.status === 204) return undefined as T

  const text = await res.text()
  let data: unknown = null
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    // non-JSON error body; fall through
  }

  if (!res.ok) {
    const msg =
      data && typeof data === "object" && "error" in data
        ? String((data as { error: unknown }).error)
        : `request failed (${res.status})`
    throw new ApiError(res.status, msg)
  }
  return data as T
}

// --- Auth -------------------------------------------------------------------

export function login(username: string, password: string) {
  return request<{ token: string; user: User }>("/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  })
}

export function register(username: string, password: string) {
  return request<User>("/auth/register", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  })
}

// --- Media ------------------------------------------------------------------

export function listMedia(type: MediaType | "", limit: number, offset: number) {
  const q = new URLSearchParams({ limit: String(limit), offset: String(offset) })
  if (type) q.set("type", type)
  return request<MediaList>(`/media?${q}`)
}

export function getMedia(id: string) {
  return request<MediaItem>(`/media/${id}`)
}

export function deleteMedia(id: string) {
  return request<void>(`/media/${id}`, { method: "DELETE" })
}

// Media elements can't send Authorization headers, so these URLs carry the JWT.
function tokenQuery(): string {
  return `?token=${encodeURIComponent(getToken() ?? "")}`
}

export function thumbUrl(id: string): string {
  return `${BASE}/media/thumb/${id}${tokenQuery()}`
}

export function rawUrl(id: string): string {
  return `${BASE}/media/raw/${id}${tokenQuery()}`
}

export function hlsMasterUrl(id: string): string {
  return `${BASE}/media/stream/${id}/master.m3u8${tokenQuery()}`
}

export function wsUrl(): string {
  const proto = location.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${location.host}${BASE}/ws${tokenQuery()}`
}

// --- Resumable chunked upload -------------------------------------------------

export interface UploadProgress {
  sentBytes: number
  totalBytes: number
  percent: number
}

const CHUNK_RETRIES = 3

function putChunk(
  sessionID: string,
  index: number,
  blob: Blob,
  onBytes: (loaded: number) => void,
  signal?: AbortSignal
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open("PUT", `${BASE}/uploads/${sessionID}/chunks/${index}`)
    xhr.setRequestHeader("Authorization", `Bearer ${getToken() ?? ""}`)
    xhr.upload.onprogress = (e) => onBytes(e.loaded)
    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) resolve()
      else reject(new ApiError(xhr.status, `chunk ${index} failed (${xhr.status})`))
    }
    xhr.onerror = () => reject(new Error(`chunk ${index}: network error`))
    xhr.onabort = () => reject(new DOMException("upload aborted", "AbortError"))
    signal?.addEventListener("abort", () => xhr.abort(), { once: true })
    xhr.send(blob)
  })
}

/**
 * Uploads a file through the resumable chunked-upload API and returns the
 * created MediaItem. Chunks stream straight from the File object (never fully
 * buffered in memory) and each chunk is retried with backoff before giving up.
 */
export async function uploadFile(
  file: File,
  onProgress: (p: UploadProgress) => void,
  signal?: AbortSignal
): Promise<MediaItem> {
  const session = await request<UploadSession>("/uploads", {
    method: "POST",
    body: JSON.stringify({
      filename: file.name,
      title: file.name.replace(/\.[^.]+$/, ""),
      mime_type: file.type,
      size: file.size,
    }),
    signal,
  })

  const received = new Set(session.received_chunks)
  let sentBytes = 0
  for (const idx of received) {
    const start = idx * session.chunk_size
    sentBytes += Math.min(session.chunk_size, file.size - start)
  }

  for (let idx = 0; idx < session.total_chunks; idx++) {
    if (received.has(idx)) continue
    const start = idx * session.chunk_size
    const blob = file.slice(start, Math.min(start + session.chunk_size, file.size))

    let lastErr: unknown = null
    for (let attempt = 0; attempt <= CHUNK_RETRIES; attempt++) {
      if (signal?.aborted) throw new DOMException("upload aborted", "AbortError")
      try {
        const base = sentBytes
        await putChunk(
          session.id,
          idx,
          blob,
          (loaded) => {
            const total = Math.min(base + loaded, file.size)
            onProgress({
              sentBytes: total,
              totalBytes: file.size,
              percent: file.size > 0 ? (total / file.size) * 100 : 100,
            })
          },
          signal
        )
        lastErr = null
        break
      } catch (err) {
        if (err instanceof DOMException && err.name === "AbortError") throw err
        lastErr = err
        await new Promise((r) => setTimeout(r, 500 * 2 ** attempt))
      }
    }
    if (lastErr) throw lastErr

    sentBytes += blob.size
    onProgress({
      sentBytes,
      totalBytes: file.size,
      percent: file.size > 0 ? (sentBytes / file.size) * 100 : 100,
    })
  }

  return request<MediaItem>(`/uploads/${session.id}/complete`, {
    method: "POST",
    signal,
  })
}

// --- Formatting helpers -------------------------------------------------------

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  const units = ["KB", "MB", "GB", "TB"]
  let v = n
  let u = -1
  do {
    v /= 1024
    u++
  } while (v >= 1024 && u < units.length - 1)
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[u]}`
}

export function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0) return ""
  const s = Math.round(seconds)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  return h > 0
    ? `${h}:${String(m).padStart(2, "0")}:${String(sec).padStart(2, "0")}`
    : `${m}:${String(sec).padStart(2, "0")}`
}
