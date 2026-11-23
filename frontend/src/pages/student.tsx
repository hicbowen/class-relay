import { ArrowRight, Clock, GraduationCap, LoaderCircle, Radio, ShieldCheck, WifiOff } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, errorMessage } from '../api'
import { Brand, ConnectionBadge, ThemeButton } from '../components/common'
import { Conversation } from '../components/conversation'
import { Button, Dialog, ErrorNotice } from '../components/ui'
import { useApp } from '../store'
import { useRoom } from '../use-room'

export function StudentPage() {
  const session = useRoom('student')
  return <StudentRoomView key={session.room?.lessonId ?? ''} session={session} />
}

function StudentRoomView({ session }: { session: ReturnType<typeof useRoom<'student'>> }) {
  const { room, connection, error, revision, refresh, retry } = session
  const [name, setName] = useState('')
  const [pin, setPIN] = useState('')
  const [formError, setFormError] = useState('')
  const [busy, setBusy] = useState(false)
  const [reapply, setReapply] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const student = room?.student
  const identity = student?.status === 'approved' ? student.id : null
  const generation = room?.generation
  useEffect(() => () => {
    if (identity && generation) useApp.getState().forget(`student:${identity}:${generation}:`)
  }, [identity, generation])

  async function join() {
    setBusy(true)
    setFormError('')
    try {
      await api('student', 'join', { name, pin, lessonId: room?.lessonId })
      setPIN('')
      setReapply(false)
      refresh()
    } catch (err) { setFormError(errorMessage(err)) }
    finally { setBusy(false) }
  }

  async function leave() {
    setBusy(true)
    setFormError('')
    try {
      await api('student', 'leave', { studentId: student?.id })
      setName('')
      setPIN('')
      setConfirm(false)
      refresh()
    } catch (err) { setFormError(errorMessage(err)); refresh() }
    finally { setBusy(false) }
  }

  const changeStudent = <Button variant="outline" size="small" onClick={() => { setFormError(''); setConfirm(true) }}>更换学生</Button>
  const revoked = student?.status === 'denied' || student?.status === 'removed'
  return <div className="student-shell">
    <header className="app-topbar">
      <Brand /><div className="topbar-actions"><ConnectionBadge state={connection} /><span className="topbar-divider" /><ThemeButton /></div>
    </header>
    {room && identity ? <main className="student-chat chat-panel">
      <header className="chat-header">
        <span className="avatar broadcast-avatar"><Radio size={20} /></span>
        <div className="chat-title"><h1>{room.name}</h1><p>{student?.name} · 学生</p></div>
        {room.cloud && changeStudent}
      </header>
      {connection === 'offline' && <div className="connection-banner" role="status"><WifiOff size={16} /> 连接已断开，正在尝试重新连接。<Button variant="ghost" size="small" onClick={retry}>立即重连</Button></div>}
      <Conversation key={`${identity}:${room.generation}`} id="classroom" role="student" identity={identity}
        name={room.name} generation={room.generation} revision={revision} connection={connection}
        disabled={room.sending ? undefined : '教师已关闭学生发送权限。'} />
    </main> : <main className="student-entrance">
      <section className="entry-card">
        <div className="entry-symbol">{student?.status === 'pending' ? <Clock size={28} /> : <GraduationCap size={32} />}</div>
        {!room ? <>
          <h1>{error ? '暂时无法连接课堂' : '正在连接课堂'}</h1>
          {error ? <><ErrorNotice>{error}</ErrorNotice><Button variant="outline" onClick={retry}>重新连接</Button></> : <LoaderCircle className="spin" size={23} />}
        </> : room.cloud && !room.lessonId ? <>
          <h1>等待老师开始上课</h1><p className="entry-subtitle">开课后，在这个页面填写姓名和本节课口令。</p>
        </> : student?.status === 'pending' ? <>
          <h1>等待教师批准</h1><p className="entry-subtitle">你已用「{student.name}」申请加入{room.name}。批准后会自动进入课堂。</p>
          <div className="pending-indicator"><i /> {connection === 'offline' ? '连接断开，正在重连' : '已发送加入申请'}</div>
          {room.cloud && changeStudent}
        </> : revoked && !reapply ? <>
          <h1>{student.status === 'removed' ? '你已被移出课堂' : '加入申请未通过'}</h1>
          <p className="entry-subtitle">可以向教师确认课堂信息后重新申请。</p>
          <Button onClick={() => { setName(room.cloud ? '' : student.name); setReapply(true) }}>重新申请<ArrowRight size={17} /></Button>
        </> : <>
          <h1>加入课堂</h1><p className="entry-subtitle">输入姓名和教师提供的课堂口令，加入{room.name}。</p>
          <form className="join-form" onSubmit={event => { event.preventDefault(); void join() }}>
            <label className="field">学生姓名<input placeholder="输入你的姓名" autoComplete={room.cloud ? "off" : "name"} required maxLength={40} value={name} onChange={event => setName(event.target.value)} /></label>
            <label className="field">课堂口令<input placeholder="输入 6 位数字" autoComplete="off" inputMode="numeric" pattern="[0-9]{6}" required maxLength={6} value={pin} onChange={event => setPIN(event.target.value.replace(/\D/g, ''))} /></label>
            <ErrorNotice>{formError || error}</ErrorNotice>
            <Button className="join-button" type="submit" disabled={busy || connection !== 'online' || !name.trim() || pin.length !== 6}>
              {busy ? <><LoaderCircle className="spin" size={17} />正在申请…</> : <>申请加入 <ArrowRight size={17} /></>}
            </Button>
          </form>
        </>}
      </section>
      {!room?.cloud && <p className="student-network-note"><ShieldCheck size={15} />请在可信局域网使用，避免发送敏感信息。</p>}
    </main>}
    {room && !room.cloud && identity && <div className="student-chat-note"><ShieldCheck size={13} />课堂消息经局域网 HTTP 传输，请避免发送敏感信息。</div>}
    {confirm && <Dialog title="更换学生" description="退出当前身份后，下一位学生可用自己的姓名重新签到。" onClose={() => setConfirm(false)}>
      <ErrorNotice>{formError}</ErrorNotice>
      <div className="dialog-actions">
        <Button variant="outline" onClick={() => setConfirm(false)}>取消</Button>
        <Button disabled={busy} onClick={() => void leave()}>退出并重新签到</Button>
      </div>
    </Dialog>}
  </div>
}
