import { useEffect, useRef, useState } from "react"
import { wsUrl, type MediaEvent } from "@/lib/api"

/**
 * Maintains the WebSocket connection to /api/v1/ws and forwards processing
 * events. Reconnects with capped exponential backoff; `connected` drives the
 * status indicator in the header.
 */
export function useMediaEvents(
  enabled: boolean,
  onEvent: (evt: MediaEvent) => void
): boolean {
  const [connected, setConnected] = useState(false)
  const handler = useRef(onEvent)
  handler.current = onEvent

  useEffect(() => {
    if (!enabled) return

    let ws: WebSocket | null = null
    let closed = false
    let attempts = 0
    let timer: ReturnType<typeof setTimeout> | undefined

    const connect = () => {
      ws = new WebSocket(wsUrl())

      ws.onopen = () => {
        attempts = 0
        setConnected(true)
      }
      ws.onmessage = (msg) => {
        try {
          handler.current(JSON.parse(msg.data as string) as MediaEvent)
        } catch {
          // ignore malformed frames
        }
      }
      ws.onclose = () => {
        setConnected(false)
        if (closed) return
        const delay = Math.min(1000 * 2 ** attempts, 15000)
        attempts++
        timer = setTimeout(connect, delay)
      }
      ws.onerror = () => ws?.close()
    }

    connect()

    return () => {
      closed = true
      clearTimeout(timer)
      ws?.close()
      setConnected(false)
    }
  }, [enabled])

  return connected
}
