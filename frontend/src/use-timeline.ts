import { useCallback, useEffect, useRef, useState } from 'react'
import { api, errorMessage } from './api'
import type { Message, Page, Receipt, Role } from './model'

function merge(current: Message[], incoming: Message[]) {
  const messages = new Map(current.map(message => [message.id, message]))
  for (const item of incoming) messages.set(item.id, item)
  return [...messages.values()].sort((a, b) => a.seq - b.seq)
}

export function useTimeline(role: Role, conversation: string, generation: number, revision: number, studentId: string, lesson?: string) {
  const [messages, setMessages] = useState<Message[]>([])
  const [more, setMore] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingOlder, setLoadingOlder] = useState(false)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  const after = useRef<number | null>(null)
  const current = useRef<Message[]>([])
  const acknowledged = useRef(new Set<string>())
  const controller = useRef<AbortController | null>(null)
  const pending = useRef(false)
  const dirty = useRef(false)
  const loadInProgress = useRef(false)
  const query = new URLSearchParams({ conversation })
  if (lesson) query.set('lesson', lesson)
  if (role === 'student') query.set('studentId', studentId)
  const base = `messages?${query}`

  const update = useCallback((items: Message[]) => {
    current.current = merge(current.current, items)
    setMessages(current.current)
  }, [])

  const confirm = useCallback(async (signal: AbortSignal, checkReceipts: boolean) => {
    if (lesson) return
    const own = current.current.filter(item => item.requestId)
    const incoming = current.current.filter(item => !item.requestId && !acknowledged.current.has(item.id))
    for (let offset = 0; offset < incoming.length; offset += 100) {
      const ids = incoming.slice(offset, offset + 100).map(item => item.id)
      await api(role, 'ack', { ids }, signal)
      if (signal.aborted) return
      for (const id of ids) acknowledged.current.add(id)
    }
    if (!checkReceipts) return
    const unconfirmed = own.filter(item => item.delivered < item.recipients)
    for (let offset = 0; offset < unconfirmed.length; offset += 100) {
      const ids = unconfirmed.slice(offset, offset + 100).map(item => item.id)
      const states = await api<Receipt[]>(role, 'receipts', { ids }, signal)
      if (signal.aborted || !states.length) continue
      const receipts = new Map(states.map(item => [item.id, item]))
      const changed = current.current.filter(item => (receipts.get(item.id)?.delivered ?? 0) > item.delivered)
      if (changed.length) update(changed.map(item => ({ ...item, ...receipts.get(item.id)! })))
    }
  }, [role, update, lesson])

  useEffect(() => {
    controller.current = new AbortController()
    const lifetime = controller.current
    return () => lifetime.abort()
  }, [])

  useEffect(() => {
    const signal = controller.current!.signal
    const synchronize = async () => {
      if (pending.current) { dirty.current = true; return }
      pending.current = true
      try {
        do {
          dirty.current = false
          const initial = after.current === null
          let next = after.current
          let morePages: boolean
          do {
            const page: Page = await api<Page>(role, base + (next === null ? '' : `&after=${next}`), undefined, signal)
            if (signal.aborted || page.generation !== generation) return
            if (next === null) setMore(page.more)
            if (page.messages.length) update(page.messages)
            next = page.messages.at(-1)?.seq ?? next ?? 0
            after.current = next
            morePages = !initial && page.more
          } while (morePages)
          setLoading(false)
          await confirm(signal, true)
          if (!signal.aborted) setError('')
        } while (dirty.current && !signal.aborted)
      } catch (err) {
        if (!signal.aborted) { setError(errorMessage(err)); setLoading(false) }
      } finally {
        pending.current = false
      }
    }
    void synchronize()
  }, [role, base, generation, revision, retry, update, confirm])

  const loadOlder = async () => {
    const before = current.current[0]?.seq
    if (!before || loadInProgress.current) return
    loadInProgress.current = true
    setLoadingOlder(true)
    const signal = controller.current!.signal
    try {
      const page = await api<Page>(role, base + `&before=${before}`, undefined, signal)
      if (signal.aborted || page.generation !== generation) return
      update(page.messages)
      setMore(page.more)
      setError('')
      await confirm(signal, false)
    } catch (err) {
      if (!signal.aborted) setError(errorMessage(err))
    } finally {
      loadInProgress.current = false
      if (!signal.aborted) setLoadingOlder(false)
    }
  }

  return { messages, more, loading, loadingOlder, error, loadOlder, update, refresh: () => setRetry(v => v + 1) }
}
