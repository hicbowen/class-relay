import { useCallback, useEffect, useRef, useState } from 'react'
import { api, APIError, errorMessage } from './api'
import type { Connection, Role, StudentRoom, TeacherRoom } from './model'

type State<R extends Role> = R extends 'teacher' ? TeacherRoom : StudentRoom

export function useRoom<R extends Role>(role: R) {
  const [room, setRoom] = useState<State<R> | null>(null)
  const [error, setError] = useState('')
  const [connection, setConnection] = useState<Connection>('connecting')
  const [revision, setRevision] = useState(0)
  const [reload, setReload] = useState(0)
  const fresh = useRef<() => void>(() => {})
  const refresh = useCallback(() => fresh.current(), [])
  const retry = useCallback(() => setReload(v => v + 1), [])

  useEffect(() => {
    const abort = new AbortController()
    const { signal } = abort
    let stream: EventSource | undefined
    let reconnectTimer: ReturnType<typeof setTimeout> | undefined
    let refreshTimer: ReturnType<typeof setTimeout> | undefined
    let lastNetwork = 0
    let failures = 0
    let active = false
    let dirty = false
    let subscribed = false
    let gotState = false
    let lastError = ''

    function closeStream() {
      stream?.close()
      stream = undefined
    }

    function reconnect() {
      if (signal.aborted) return
      clearTimeout(reconnectTimer)
      setConnection('offline')
      closeStream()
      reconnectTimer = setTimeout(() => {
        if (subscribed && navigator.onLine) connect()
        schedule()
      }, Math.min(700 * 2 ** failures++, 8000))
    }

    function connect() {
      if (signal.aborted || stream || !navigator.onLine) return
      const current = new EventSource(`/api/${role}/events`)
      stream = current
      lastNetwork = Date.now()
      current.onopen = () => {
        if (stream !== current) return
        lastNetwork = Date.now()
        failures = 0
        setConnection('online')
      }
      const sync = () => {
        if (stream !== current) return
        lastNetwork = Date.now()
        schedule()
      }
      current.addEventListener('sync', sync)
      current.addEventListener('heartbeat', sync)
      current.onerror = () => {
        if (stream !== current) return
        schedule()
        reconnect()
      }
    }

    async function load() {
      if (signal.aborted || active) return
      active = true
      dirty = false
      try {
        const data = await api<State<R>>(role, 'state', undefined, signal)
        if (signal.aborted) return
        gotState = true
        setRoom(data)
        if (!lastError) setError('')
        const student = 'student' in data ? data.student : null
        const listen = role === 'teacher' || student?.status === 'pending' || student?.status === 'approved'
        setRevision(value => value + 1)
        if (listen && !subscribed) {
          subscribed = true
          setConnection(navigator.onLine ? 'connecting' : 'offline')
          connect()
        }
        if (!listen) {
          subscribed = false
          closeStream()
          clearTimeout(reconnectTimer)
          setConnection('online')
        }
      } catch (err) {
        if (signal.aborted) return
        if (err instanceof APIError && err.status === 401) {
          setRoom(null)
          subscribed = false
          closeStream()
          clearTimeout(reconnectTimer)
        }
        setError(lastError || errorMessage(err))
        setConnection('offline')
      } finally {
        active = false
        if (dirty) schedule()
      }
    }

    function schedule() {
      if (signal.aborted) return
      if (active) { dirty = true; return }
      if (refreshTimer !== undefined) return
      refreshTimer = setTimeout(() => {
        refreshTimer = undefined
        void load()
      }, 60)
    }

    async function start() {
      if (role === 'teacher' && location.hash) {
        const key = new URLSearchParams(location.hash.slice(1)).get('key')
        if (key) {
          history.replaceState(null, '', location.pathname + location.search)
          try { await api(role, 'login', { key }, signal) } catch (err) {
            if (!signal.aborted) lastError = errorMessage(err)
          }
        }
      }
      await load()
    }

    function offline() {
      closeStream()
      clearTimeout(reconnectTimer)
      setConnection('offline')
    }
    function online() {
      failures = 0
      if (subscribed) connect()
      schedule()
    }
    fresh.current = schedule
    const watchdog = setInterval(() => {
      if (stream && Date.now() - lastNetwork > 24_000) reconnect()
      if ((!stream && subscribed) || !gotState || role === 'student' && !subscribed) schedule()
    }, 4000)
    window.addEventListener('offline', offline)
    window.addEventListener('online', online)
    void start()
    return () => {
      abort.abort()
      closeStream()
      clearInterval(watchdog)
      clearTimeout(reconnectTimer)
      clearTimeout(refreshTimer)
      window.removeEventListener('offline', offline)
      window.removeEventListener('online', online)
    }
  }, [role, reload])

  return { room, error, connection, revision, refresh, retry }
}
