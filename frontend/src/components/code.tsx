import { useEffect, useState } from 'react'
import { createHighlighterCore, type ThemedToken } from 'shiki/core'
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript'

const grammars = {
  python: () => import('@shikijs/langs/python'),
  cpp: () => import('@shikijs/langs/cpp'),
  c: () => import('@shikijs/langs/c'),
  javascript: () => import('@shikijs/langs/javascript'),
  typescript: () => import('@shikijs/langs/typescript'),
  go: () => import('@shikijs/langs/go'),
  java: () => import('@shikijs/langs/java'),
  html: () => import('@shikijs/langs/html'),
  css: () => import('@shikijs/langs/css'),
  json: () => import('@shikijs/langs/json'),
  bash: () => import('@shikijs/langs/bash'),
  sql: () => import('@shikijs/langs/sql'),
}
type Language = keyof typeof grammars
let highlighter: ReturnType<typeof createHighlighterCore> | undefined
const ready = new Map<Language, Promise<void>>()

async function highlight(content: string, language: Language) {
  highlighter ??= createHighlighterCore({
    themes: [import('@shikijs/themes/github-light'), import('@shikijs/themes/github-dark')],
    langs: [],
    engine: createJavaScriptRegexEngine(),
  })
  const shiki = await highlighter
  let loaded = ready.get(language)
  if (!loaded) {
    loaded = grammars[language]().then(module => shiki.loadLanguage(module.default))
    ready.set(language, loaded)
  }
  await loaded
  return shiki.codeToTokens(content, {
    lang: language,
    themes: { light: 'github-light', dark: 'github-dark' },
    defaultColor: false,
    tokenizeMaxLineLength: 1600,
  }).tokens
}

export default function SyntaxCode({ content, language }: { content: string; language: string }) {
  const [syntax, setSyntax] = useState<ThemedToken[][] | null>(null)
  useEffect(() => {
    if (!Object.hasOwn(grammars, language)) return
    let alive = true
    void highlight(content, language as Language).then(tokens => { if (alive) setSyntax(tokens) }).catch(() => {})
    return () => { alive = false }
  }, [content, language])
  return syntax ? syntax.map((line, i) => <span key={i}>
    {line.map((token, j) => <span key={j} style={token.htmlStyle}>{token.content}</span>)}{i < syntax.length - 1 && '\n'}
  </span>) : content
}
