import type { Role } from './model'

export class APIError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

export async function api<T>(role: Role, route: string, body?: object, signal?: AbortSignal): Promise<T> {
  const timeout = new AbortController()
  const abort = () => timeout.abort()
  if (signal?.aborted) abort()
  signal?.addEventListener('abort', abort, { once: true })
  const timer = setTimeout(abort, 12_000)
  try {
    const response = await fetch(`/api/${role}/${route}`, {
      method: body ? 'POST' : 'GET',
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body && JSON.stringify(body),
      signal: timeout.signal,
    })
    if (!response.ok) {
      const result: { error?: string } = await response.json().catch(() => ({}))
      throw new APIError(response.status, result.error || '操作失败，请稍后重试')
    }
    return await response.json() as T
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener('abort', abort)
  }
}

export function errorMessage(error: unknown) {
  return error instanceof APIError ? error.message : '连接失败，请检查网络后重试'
}

export function requestID() {
  // getRandomValues is available on LAN HTTP, where randomUUID may be unavailable.
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), value => value.toString(16).padStart(2, '0')).join('')
}
