import { ArrowLeft, LogIn, Play, Square } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, errorMessage } from '../api'
import { Conversation } from '../components/conversation'
import { Button, Dialog, ErrorNotice } from '../components/ui'
import type { ArchivedLesson, Lesson } from '../model'

export function CloudLogin({ refresh }: { refresh: () => void }) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function login() {
    setBusy(true)
    setError('')
    try { await api('teacher', 'login', { username, password }); refresh() }
    catch (err) { setError(errorMessage(err)) }
    finally { setBusy(false) }
  }
  return <main className="student-entrance">
    <section className="entry-card">
      <div className="entry-symbol"><LogIn size={26} /></div>
      <h1>教师登录</h1>
      <form className="join-form" onSubmit={event => { event.preventDefault(); void login() }}>
        <label className="field">教师账号<input autoComplete="username" required value={username} onChange={event => setUsername(event.target.value)} /></label>
        <label className="field">密码<input type="password" autoComplete="current-password" required value={password} onChange={event => setPassword(event.target.value)} /></label>
        <ErrorNotice>{error}</ErrorNotice>
        <Button type="submit" className="join-button" disabled={busy || !username.trim() || !password}>{busy ? '正在登录…' : '登录'}</Button>
      </form>
    </section>
  </main>
}

export function StartLesson({ refresh }: { refresh: () => void }) {
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function start() {
    setBusy(true)
    setError('')
    try { await api('teacher', 'lessons/start', { name }); refresh() }
    catch (err) { setError(errorMessage(err)); refresh() }
    finally { setBusy(false) }
  }
  return <main className="student-entrance">
    <section className="entry-card">
      <div className="entry-symbol"><Play size={27} /></div>
      <h1>开始上课</h1>
      <p className="entry-subtitle">学生用固定链接访问。开课后，向学生提供本节口令。</p>
      <form className="join-form" onSubmit={event => { event.preventDefault(); void start() }}>
        <label className="field">课堂名称<input required maxLength={40} value={name} placeholder="如：三年级信息课" onChange={event => setName(event.target.value)} /></label>
        <ErrorNotice>{error}</ErrorNotice>
        <Button type="submit" className="join-button" disabled={busy || !name.trim()}>开始上课</Button>
      </form>
    </section>
  </main>
}

export function EndLesson({ id, refresh, onClose }: { id: string; refresh: () => void; onClose: () => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function end() {
    setBusy(true)
    setError('')
    try { await api('teacher', 'lessons/end', { lessonId: id }); refresh(); onClose() }
    catch (err) { setError(errorMessage(err)); refresh() }
    finally { setBusy(false) }
  }
  return <Dialog title="结束课堂" description="学生会退出本节课。学生名单和消息将保存在上课记录中。" onClose={onClose}>
    <ErrorNotice>{error}</ErrorNotice>
    <div className="dialog-actions">
      <Button variant="outline" onClick={onClose}>取消</Button>
      <Button variant="danger" disabled={busy} onClick={() => void end()}><Square size={14} />确认结束</Button>
    </div>
  </Dialog>
}

const clock = new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'short' })

export function LessonHistory({ generation, onClose }: { generation: number; onClose: () => void }) {
  const [lessons, setLessons] = useState<Lesson[]>([])
  const [archive, setArchive] = useState<ArchivedLesson | null>(null)
  const [selected, setSelected] = useState('all')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [more, setMore] = useState(false)

  useEffect(() => {
    const abort = new AbortController()
    setBusy(true)
    void api<Lesson[]>('teacher', 'lessons', undefined, abort.signal).then(data => {
      if (abort.signal.aborted) return
      setLessons(data)
      setMore(data.length === 50)
    }).catch(err => { if (!abort.signal.aborted) setError(errorMessage(err)) })
      .finally(() => { if (!abort.signal.aborted) setBusy(false) })
    return () => abort.abort()
  }, [])

  async function older() {
    const cursor = lessons.at(-1)?.id
    if (!cursor) return
    setBusy(true)
    setError('')
    try {
      const data = await api<Lesson[]>('teacher', `lessons?before=${encodeURIComponent(cursor)}`)
      setLessons(items => [...items, ...data])
      setMore(data.length === 50)
    } catch (err) { setError(errorMessage(err)) }
    finally { setBusy(false) }
  }

  async function open(id: string) {
    setBusy(true)
    setError('')
    try {
      setArchive(await api<ArchivedLesson>('teacher', `lessons/${id}`))
      setSelected('all')
    } catch (err) { setError(errorMessage(err)) }
    finally { setBusy(false) }
  }

  return <Dialog title="上课记录" description={archive ? `${archive.name} · ${clock.format(archive.createdAt)}` : '查看已结束课堂的广播和私聊。'} onClose={onClose}>
    <ErrorNotice>{error}</ErrorNotice>
    {archive ? <>
      <Button variant="ghost" size="small" onClick={() => setArchive(null)}><ArrowLeft size={15} />返回记录</Button>
      <label className="field">会话
        <select value={selected} onChange={event => setSelected(event.target.value)}>
          <option value="all">全班广播</option>
          {archive.students.filter(student => student.status !== 'pending' || student.unread > 0).map(student => <option key={student.id} value={student.id}>{student.name}</option>)}
        </select>
      </label>
      <div className="archive-chat">
        <Conversation key={`${archive.id}:${selected}:${generation}`} id={selected} role="teacher" identity="teacher"
          name={archive.students.find(student => student.id === selected)?.name ?? archive.name} generation={generation} revision={0}
          connection="online" lesson={archive.id} disabled="课堂已结束，可以查看和复制历史消息。" />
      </div>
    </> : <div className="request-list">
      {!lessons.length && !error && <div className="status-panel">{busy ? '正在加载…' : '暂无已结束的课堂'}</div>}
      {lessons.map(lesson => <button type="button" key={lesson.id} className="student-row archive-row" disabled={busy} onClick={() => void open(lesson.id)}>
        <strong>{lesson.name}</strong><time dateTime={new Date(lesson.createdAt).toISOString()}>{clock.format(lesson.createdAt)}</time>
      </button>)}
      {more && <Button variant="outline" size="small" disabled={busy} onClick={() => void older()}>更早的课堂</Button>}
    </div>}
  </Dialog>
}
