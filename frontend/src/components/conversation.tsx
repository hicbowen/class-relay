import { Check, CheckCheck, ChevronUp, Clipboard, Code2, CornerDownLeft, LoaderCircle, MessageCircle, RotateCw, Send, Type, X } from 'lucide-react'
import { lazy, Suspense, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { api, errorMessage, requestID } from '../api'
import type { Connection, Message, Role, Submission } from '../model'
import { languages } from '../model'
import { emptyDraft, useApp } from '../store'
import { useTimeline } from '../use-timeline'
import { CopyFeedback, copyText } from './common'
import { Button, ErrorNotice } from './ui'

const SyntaxCode = lazy(() => import('./code').catch(() => ({ default: ({ content }: { content: string }) => content })))

function CodeBlock({ content, language = 'plaintext' }: { content: string; language?: string }) {
  const label = languages.find(([id]) => id === language)?.[1] ?? '代码'
  return <div className="code-block">
    <div className="code-label">{label}</div>
    <pre tabIndex={0} aria-label={`${label} 代码，可滚动`}><code>{language === 'plaintext' ? content :
      <Suspense fallback={content}><SyntaxCode content={content} language={language} /></Suspense>}</code></pre>
  </div>
}

const emptyQueue: ReturnType<typeof useApp.getState>['outgoing'][string] = []
const encoder = new TextEncoder()
const clock = new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit', hour12: false })
const day = new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: 'long', day: 'numeric' })

function dateLabel(timestamp: number) {
  const today = new Date()
  if (today.toDateString() === new Date(timestamp).toDateString()) return '今天'
  today.setDate(today.getDate() - 1)
  if (today.toDateString() === new Date(timestamp).toDateString()) return '昨天'
  return day.format(timestamp)
}

function MessageBubble({ item, role, name }: { item: Message; role: Role; name: string }) {
  const own = Boolean(item.requestId)
  const [copied, setCopied] = useState<'' | 'copied' | 'manual'>('')
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(timer.current), [])
  const status = item.receiverId === 'all'
    ? `已送达 ${item.delivered} / ${item.recipients}`
    : item.delivered ? '已送达' : '已发送'
  const label = own ? '我' : role === 'teacher' ? name : item.receiverId === 'all' ? '教师 · 全班广播' : '教师 · 私信'
  return <article className={`message ${own ? 'message-own' : ''}`}>
    <div className="message-meta"><strong>{label}</strong><time dateTime={new Date(item.createdAt).toISOString()}>{clock.format(item.createdAt)}</time></div>
    <div className={`bubble ${item.type === 'code' ? 'bubble-code' : ''}`}>
      {item.type === 'code' ? <CodeBlock content={item.content} language={item.language} /> : <div className="message-text">{item.content}</div>}
    </div>
    <div className="message-tools">
      {own && <span className="delivery">{item.delivered ? <CheckCheck size={14} /> : <Check size={14} />}{status}</span>}
      <button type="button" className="copy-button" aria-label="复制消息" onClick={async () => {
        setCopied(await copyText(item.content) ? 'copied' : 'manual')
        clearTimeout(timer.current)
        timer.current = setTimeout(() => setCopied(''), 2600)
      }}><Clipboard size={13} />复制</button>
      <CopyFeedback status={copied} />
    </div>
  </article>
}

type ConversationProps = {
  id: string
  role: Role
  identity: string
  name: string
  generation: number
  revision: number
  connection: Connection
  disabled?: string
  lesson?: string
}

export function Conversation({ id, role, identity, name, generation, revision, connection, disabled, lesson }: ConversationProps) {
  const timeline = useTimeline(role, id, generation, revision, identity, lesson)
  const { messages, loadOlder, refresh } = timeline
  const key = `${role}:${identity}:${generation}:${id}`
  const draft = useApp(state => state.drafts[key] ?? emptyDraft)
  const setDraft = useApp(state => state.setDraft)
  const outgoing = useApp(state => state.outgoing[key] ?? emptyQueue)
  const queue = useApp(state => state.queue)
  const dismiss = useApp(state => state.dismiss)
  const scroll = useRef<HTMLDivElement>(null)
  const mounted = useRef(false)
  const beforeOlder = useRef<{ height: number; top: number } | null>(null)
  const [atBottom, setAtBottom] = useState(true)
  const sticky = useRef(true)
  const [newCount, setNewCount] = useState(0)
  const lastSeq = useRef(0)
  const [visible, setVisible] = useState(document.visibilityState === 'visible')
  const seen = useRef(0)
  const seeing = useRef(false)
  const textArea = useRef<HTMLTextAreaElement>(null)
  const bytes = encoder.encode(draft.content).length
  const tooLarge = bytes > 65_536
  const isCode = draft.type === 'code'
  const canSend = !disabled && connection === 'online' && !timeline.loading
  const sendable = canSend && !tooLarge && draft.content.trim() !== ''

  useEffect(() => {
    mounted.current = true
    function visibility() { setVisible(document.visibilityState === 'visible') }
    document.addEventListener('visibilitychange', visibility)
    return () => {
      mounted.current = false
      document.removeEventListener('visibilitychange', visibility)
    }
  }, [])

  useLayoutEffect(() => {
    const node = scroll.current
    if (!node) return
    if (beforeOlder.current && !timeline.loadingOlder) {
      node.scrollTop = beforeOlder.current.top + node.scrollHeight - beforeOlder.current.height
      beforeOlder.current = null
    } else if (sticky.current || !lastSeq.current) {
      node.scrollTop = node.scrollHeight
    }
    const newest = messages.at(-1)?.seq ?? 0
    if (!sticky.current && lastSeq.current && newest > lastSeq.current) {
      const count = messages.filter(v => v.seq > lastSeq.current && !v.requestId).length
      if (count) setNewCount(value => value + count)
    }
    lastSeq.current = newest
  }, [messages, outgoing, timeline.loadingOlder])

  useEffect(() => {
    if (lesson || role !== 'teacher' || id === 'all' || !visible || !atBottom || connection !== 'online' || seeing.current) return
    const newest = messages.reduce((cursor, item) => item.senderId === id ? item.seq : cursor, 0)
    if (newest <= seen.current) return
    seeing.current = true
    void api(role, 'seen', { studentId: id, seq: newest }).then(() => {
      seen.current = newest
      if (mounted.current) refresh()
    }).catch(() => {}).finally(() => { seeing.current = false })
  }, [role, id, lesson, visible, atBottom, connection, messages, revision, refresh])

  useEffect(() => {
    for (const item of outgoing) {
      if (messages.some(message => message.requestId === item.input.requestId)) dismiss(key, item.input.requestId)
    }
  }, [messages, outgoing, dismiss, key])

  function goBottom() {
    const node = scroll.current
    if (!node) return
    sticky.current = true
    node.scrollTop = node.scrollHeight
    setAtBottom(true)
    setNewCount(0)
  }

  async function send(input: Submission, time: number) {
    queue(key, { input, time })
    try {
      const saved = await api<Message>(role, 'messages', input)
      if (mounted.current) timeline.update([saved])
      else dismiss(key, input.requestId)
    } catch (error) {
      queue(key, { input, time, error: errorMessage(error) })
      if (mounted.current) refresh()
    }
  }

  function submit() {
    if (!sendable) return
    const input = { ...draft, receiverId: role === 'teacher' ? id : 'teacher', requestId: requestID(), generation, studentId: role === 'student' ? identity : undefined }
    setDraft(key, { ...draft, content: '' })
    goBottom()
    void send(input, Date.now())
    textArea.current?.focus()
  }

  return <>
    <div className="timeline-wrap">
      <div className="timeline" ref={scroll} tabIndex={0} aria-label="消息记录" onScroll={event => {
        const node = event.currentTarget
        const close = node.scrollHeight - node.scrollTop - node.clientHeight < 72
        sticky.current = close
        setAtBottom(close)
        if (close) setNewCount(0)
      }}>
        <div className="timeline-inner">
          {timeline.more && <div className="older"><Button variant="ghost" size="small" disabled={timeline.loadingOlder} onClick={() => {
            if (scroll.current) beforeOlder.current = { height: scroll.current.scrollHeight, top: scroll.current.scrollTop }
            void loadOlder()
          }}><ChevronUp size={15} />{timeline.loadingOlder ? '正在加载…' : '查看更早的消息'}</Button></div>}
          {timeline.error && <div className="history-error"><ErrorNotice>{timeline.error}</ErrorNotice><Button variant="ghost" size="small" onClick={refresh}>重试</Button></div>}
          {timeline.loading && <div className="empty-timeline"><LoaderCircle className="spin" size={24} /><p>正在加载消息…</p></div>}
          {!timeline.loading && !timeline.error && !messages.length && !outgoing.length && <div className="empty-timeline">
            <span className="empty-icon"><MessageCircle size={29} strokeWidth={1.6} /></span>
            <h2>还没有消息</h2>
            <p>{role === 'student' ? '老师发来的文字和代码会显示在这里。' : id === 'all' ? '在下方输入内容，向全班发送第一条消息。' : '在下方输入内容，开始这段会话。'}</p>
          </div>}
          {messages.map((item, index) => <div className="message-row" key={item.id}>
            {(index === 0 || dateLabel(messages[index - 1].createdAt) !== dateLabel(item.createdAt)) &&
              <div className="date-separator"><span>{dateLabel(item.createdAt)}</span></div>}
            <MessageBubble item={item} role={role} name={name} />
          </div>)}
          {outgoing.filter(v => !messages.some(item => item.requestId === v.input.requestId)).map(({ input, time, error }) => <article className="message message-own" key={input.requestId}>
            <div className="message-meta"><strong>我</strong><span>{clock.format(time)}</span></div>
            <div className={`bubble ${input.type === 'code' ? 'bubble-code' : ''}`}>{input.type === 'code'
              ? <CodeBlock content={input.content} language={input.language} /> : <div className="message-text">{input.content}</div>}</div>
            <div className="message-tools">
              {error ? <><span className="failed-message" title={error}>发送失败 · {error}</span>
                <button className="copy-button" type="button" disabled={!canSend} onClick={() => void send(input, time)}><RotateCw size={13} />重试</button>
                <button className="copy-button" type="button" aria-label="清除发送失败的消息" onClick={() => dismiss(key, input.requestId)}><X size={14} /></button></>
                : <span className="delivery"><LoaderCircle className="spin" size={13} />发送中</span>}
            </div>
          </article>)}
        </div>
      </div>
      {!atBottom && newCount > 0 && <Button className="new-messages" size="small" onClick={goBottom}>{newCount} 条新消息 · 跳到底部</Button>}
    </div>
    <div className="composer-zone">
      {disabled ? <div className="composer-disabled" role="status">{disabled}</div> : <form className="composer" onSubmit={event => { event.preventDefault(); submit() }}>
        <div className="composer-toolbar">
          <div className="segmented" role="group" aria-label="消息类型">
            <button type="button" aria-pressed={!isCode} onClick={() => setDraft(key, { ...draft, type: 'text' })}><Type size={15} />文本</button>
            <button type="button" aria-pressed={isCode} onClick={() => setDraft(key, { ...draft, type: 'code' })}><Code2 size={16} />代码</button>
          </div>
          {isCode && <select className="language-select" aria-label="代码语言" value={draft.language} onChange={event => setDraft(key, { ...draft, language: event.target.value })}>
            {languages.map(([value, label]) => <option key={value} value={value}>{label}</option>)}
          </select>}
        </div>
        <textarea ref={textArea} aria-label="消息内容" spellCheck={!isCode} className={isCode ? 'code-input' : ''}
          placeholder={isCode ? '粘贴或输入代码…' : role === 'student' ? '输入要发给老师的消息…' : '输入消息…'}
          value={draft.content} onChange={event => setDraft(key, { ...draft, content: event.target.value })}
          onKeyDown={event => {
            if (event.nativeEvent.isComposing || event.keyCode === 229 || event.key !== 'Enter') return
            if (isCode ? event.ctrlKey || event.metaKey : !event.shiftKey) {
              event.preventDefault()
              submit()
            }
          }} />
        <div className="composer-bottom">
          <span className={tooLarge ? 'input-error' : 'key-hint'}>{tooLarge ? '消息不能超过 64 KiB' : connection !== 'online' ? '连接恢复后可发送' : <><CornerDownLeft size={14} />{isCode ? 'Ctrl / ⌘ + Enter 发送' : 'Enter 发送 · Shift + Enter 换行'}</>}</span>
          <Button type="submit" disabled={!sendable}><Send size={15} />发送</Button>
        </div>
      </form>}
    </div>
  </>
}
