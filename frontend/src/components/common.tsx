import { Check, CircleHelp, Monitor, Moon, Orbit, Sun } from 'lucide-react'
import { useEffect } from 'react'
import type { Connection } from '../model'
import { useApp, type Theme } from '../store'

export function Brand() {
  return <div className="brand">
    <span className="brand-mark"><Orbit size={21} strokeWidth={2.2} /></span>
    <span className="brand-name">课递 <small>ClassRelay</small></span>
  </div>
}

export function ConnectionBadge({ state }: { state: Connection }) {
  const names = { connecting: '正在连接', online: '实时连接', offline: '连接已断开' }
  return <span className={`connection connection-${state}`} role="status"><i />{names[state]}</span>
}

const themes: Record<Theme, { label: string; next: Theme }> = {
  system: { label: '跟随系统', next: 'light' },
  light: { label: '浅色', next: 'dark' },
  dark: { label: '深色', next: 'system' },
}

export function ThemeButton() {
  const theme = useApp(s => s.theme)
  const setTheme = useApp(s => s.setTheme)
  const Icon = theme === 'dark' ? Moon : theme === 'light' ? Sun : Monitor
  useEffect(() => { document.documentElement.dataset.theme = theme }, [theme])
  return <button className="button button-icon button-ghost" type="button"
    aria-label={`主题：${themes[theme].label}，切换为${themes[themes[theme].next].label}`}
    title={`主题：${themes[theme].label}`} onClick={() => setTheme(themes[theme].next)}><Icon size={18} /></button>
}

export async function copyText(text: string) {
  try {
    if (navigator.clipboard) { await navigator.clipboard.writeText(text); return true }
  } catch { /* use the LAN HTTP fallback */ }
  const field = document.createElement('textarea')
  field.value = text
  field.readOnly = true
  field.style.position = 'fixed'
  field.style.opacity = '0'
  field.style.pointerEvents = 'none'
  const focused = document.activeElement
  // A modal traps focus, so insert the fallback into the modal when available.
  const parent = focused?.closest('[role="dialog"]') ?? document.body
  parent.appendChild(field)
  field.select()
  let copied = false
  try { copied = document.execCommand('copy') } catch { /* manual selection remains available */ }
  field.remove()
  if (focused instanceof HTMLElement) focused.focus()
  return copied
}

export function CopyFeedback({ status }: { status: '' | 'copied' | 'manual' }) {
  if (!status) return null
  return <span className="copy-feedback" role="status">{status === 'copied' ? <><Check size={13} /> 已复制</> : <><CircleHelp size={13} /> 请手动选中复制</>}</span>
}
