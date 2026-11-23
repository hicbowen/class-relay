import { create } from 'zustand'
import type { Draft, Submission } from './model'

export type Theme = 'system' | 'light' | 'dark'
type Outgoing = { input: Submission; time: number; error?: string }
export const emptyDraft: Draft = { type: 'text', language: 'python', content: '' }
type Preferences = {
  theme: Theme
  setTheme: (theme: Theme) => void
  drafts: Record<string, Draft>
  setDraft: (key: string, draft: Draft) => void
  outgoing: Record<string, Outgoing[]>
  queue: (key: string, item: Outgoing) => void
  dismiss: (key: string, requestId: string) => void
  forget: (key: string) => void
}

function storedTheme(): Theme {
  try {
    const value = localStorage.getItem('classrelay-theme')
    if (value === 'light' || value === 'dark') return value
  } catch { /* storage may be disabled */ }
  return 'system'
}

export const useApp = create<Preferences>(set => ({
  theme: storedTheme(),
  setTheme: theme => {
    try { localStorage.setItem('classrelay-theme', theme) } catch { /* optional preference */ }
    set({ theme })
  },
  drafts: {},
  setDraft: (key, draft) => set(state => ({ drafts: { ...state.drafts, [key]: draft } })),
  outgoing: {},
  queue: (key, item) => set(state => {
    const existing = state.outgoing[key] ?? []
    const index = existing.findIndex(v => v.input.requestId === item.input.requestId)
    const rest = existing.slice()
    if (index < 0) rest.push(item)
    else rest[index] = item
    return { outgoing: { ...state.outgoing, [key]: rest } }
  }),
  dismiss: (key, requestId) => set(state => ({
    outgoing: { ...state.outgoing, [key]: state.outgoing[key]?.filter(v => v.input.requestId !== requestId) ?? [] },
  })),
  forget: key => set(state => {
    const drafts = { ...state.drafts }
    const outgoing = { ...state.outgoing }
    for (const item of Object.keys(drafts)) if (item.startsWith(key)) delete drafts[item]
    for (const item of Object.keys(outgoing)) if (item.startsWith(key)) delete outgoing[item]
    return { drafts, outgoing }
  }),
}))
