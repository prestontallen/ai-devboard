import { useState, useEffect, useRef } from 'preact/hooks'

/**
 * Live data for the board.
 *
 * The transport is injected rather than constructed here. happy-dom, which the
 * render tests run in, provides fetch/location/matchMedia but no EventSource at
 * all — so a module that news one up cannot even be imported by a test. Passing
 * it in keeps the whole shell renderable with no server and no SSE.
 *
 * A transport is `(handlers) => teardown`, where handlers are
 * `{ onStatus(status), onChange() }`. `sseTransport` is the real one.
 */

export function sseTransport(url = '/events') {
  return ({ onStatus, onChange }) => {
    if (typeof EventSource === 'undefined') {
      onStatus('offline')
      return () => {}
    }
    const es = new EventSource(url)
    // The SSE contract carries no retry:/id: fields, so connection state is
    // derived entirely client-side from EventSource's own callbacks.
    es.onopen = () => { onStatus('live'); onChange() }
    es.onmessage = () => onChange()
    es.onerror = () => onStatus('reconnecting')
    return () => es.close()
  }
}

export async function fetchTasks(f = fetch) {
  const res = await f('/api/tasks', { cache: 'no-store' })
  if (!res.ok) throw new Error(`/api/tasks: ${res.status}`)
  return res.json()
}

/**
 * Holds the payload and connection status. `onFirstPayload` fires exactly once,
 * on the first successful load — the phone default-route rule depends on a
 * count that does not exist until then, and must not re-fire on every SSE tick.
 */
export function useBoardData({ transport, load = fetchTasks, onFirstPayload } = {}) {
  const [db, setDb] = useState({ repos: [], feedback: [] })
  const [status, setStatus] = useState('offline')
  const first = useRef(true)

  useEffect(() => {
    let live = true
    const refresh = async () => {
      try {
        const next = await load()
        if (!live) return
        setDb(next)
        if (first.current) {
          first.current = false
          if (onFirstPayload) onFirstPayload(next)
        }
      } catch {
        // Server briefly away; the transport's reconnect retriggers this.
      }
    }
    refresh()
    const teardown = transport
      ? transport({ onStatus: (s) => live && setStatus(s), onChange: refresh })
      : undefined
    return () => { live = false; if (teardown) teardown() }
  }, [transport, load])

  return { db, status }
}
