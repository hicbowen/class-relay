import { ArrowLeft, Check, ChevronRight, Clipboard, Ellipsis, LoaderCircle, LogOut, Network, Pencil, QrCode, Radio, Search, Settings, ShieldCheck, UserMinus, UserRound, UserRoundPlus, Users, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { api, errorMessage } from '../api'
import { Brand, ConnectionBadge, CopyFeedback, copyText, ThemeButton } from '../components/common'
import { Conversation } from '../components/conversation'
import { Button, Dialog, ErrorNotice } from '../components/ui'
import type { Student, TeacherRoom } from '../model'
import { useApp } from '../store'
import { useRoom } from '../use-room'
import { CloudLogin, EndLesson, LessonHistory, StartLesson } from './cloud-classroom'

type Modal = 'invite' | 'requests' | 'settings' | 'student' | 'history' | 'end' | null

function Avatar({ student }: { student: Student }) {
  return <span className={`avatar student-avatar color-${student.id.charCodeAt(0) % 4}`} aria-hidden="true">
    {[...student.name][0]}{student.online && <i />}
  </span>
}

function StudentRows({ title, students, selected, onSelect }: {
  title: string; students: Student[]; selected: string; onSelect: (id: string) => void
}) {
  if (!students.length) return null
  return <div className="student-group">
    <h2>{title}<span>{students.length}</span></h2>
    {students.map(student => <button type="button" className="student-row" key={student.id}
      aria-current={selected === student.id ? 'true' : undefined} onClick={() => onSelect(student.id)}>
      <Avatar student={student} />
      <span className="student-row-name">{student.name}</span>
      {student.unread > 0 && <span className="counter" aria-label={`${student.unread} 条未读消息`}>{student.unread > 99 ? '99+' : student.unread}</span>}
    </button>)}
  </div>
}

function Invite({ room, onClose, refresh }: { room: TeacherRoom; onClose: () => void; refresh: () => void }) {
  const [ip, setIP] = useState(room.ip ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState<'' | 'copied' | 'manual'>('')
  const [qrError, setQRError] = useState('')
  async function change(running: boolean) {
    setBusy(true)
    setError('')
    try { await api('teacher', 'network', { ip, running }); refresh() }
    catch (err) { setError(errorMessage(err)) }
    finally { setBusy(false) }
  }
  return <Dialog title="邀请学生加入" description={room.cloud ? "请学生收藏此链接，每次上课打开同一地址。" : "学生连接同一局域网后，扫码或在浏览器输入访问地址。"} onClose={onClose}>
    <div className="invite-access">
      {room.running || room.cloud ? <><div className="qr-tile">{qrError === room.url
        ? <span>二维码加载失败，请使用访问地址。</span>
        : <img src={`/api/teacher/qr?url=${encodeURIComponent(room.url)}`} width="176" height="176" alt="学生访问二维码" onError={() => setQRError(room.url)} />}</div>
        {room.pin && <div className="pin-panel"><span>{room.cloud ? "本节课口令" : "课堂口令"}</span><strong>{room.pin}</strong></div>}
        <div className="field"><label htmlFor="student-url">学生访问地址</label>
          <span className="input-row"><input id="student-url" readOnly value={room.url} onFocus={event => event.target.select()} />
            <Button variant="outline" size="icon" aria-label="复制学生访问地址" onClick={async () => setCopied(await copyText(room.url) ? 'copied' : 'manual')}><Clipboard size={17} /></Button></span>
        </div>
        <CopyFeedback status={copied} />
      </> : <div className="status-panel"><Network size={20} />局域网服务已停止</div>}
    </div>
    {!room.cloud && <form className="dialog-form" onSubmit={event => { event.preventDefault(); void change(true) }}>
      <label className="field">局域网网卡
        <select value={ip} onChange={event => setIP(event.target.value)}>
          {!room.addresses.some(address => address.ip === ip) && <option value={ip}>{ip ? `${ip}（当前不可用）` : '没有可用地址'}</option>}
          {room.addresses.map(address => <option value={address.ip} key={address.ip}>{address.ip} · {address.name}</option>)}
        </select>
      </label>
      <ErrorNotice>{error || room.networkError}</ErrorNotice>
      <p className="muted-note"><ShieldCheck size={16} />局域网 HTTP 未加密，请在可信网络使用，避免发送敏感信息。</p>
      <div className="dialog-actions">
        {room.running && <Button variant="outline" disabled={busy} onClick={() => void change(false)}>停止服务</Button>}
        <Button type="submit" disabled={busy || !room.addresses.some(address => address.ip === ip) || room.running && room.ip === ip}>
          {room.running ? '切换网卡' : '启动服务'}
        </Button>
      </div>
    </form>}
  </Dialog>
}

function Requests({ students, refresh, onClose }: { students: Student[]; refresh: () => void; onClose: () => void }) {
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  async function decide(student: Student, action: string) {
    setBusy(student.id)
    setError('')
    try { await api('teacher', `students/${student.id}`, { action }); refresh() }
    catch (err) { setError(errorMessage(err)); refresh() }
    finally { setBusy('') }
  }
  return <Dialog title="加入申请" description="批准后，学生即可接收课堂消息。" onClose={onClose}>
    <div className="request-list">
      {!students.length && <div className="status-panel"><UserRoundPlus size={19} />暂无待处理申请</div>}
      {students.map(student => <div className="request-row" key={student.id}>
        <Avatar student={student} /><span>{student.name}</span>
        <Button size="small" variant="outline" disabled={!!busy} onClick={() => void decide(student, 'deny')} aria-label={`拒绝 ${student.name}`}><X size={15} />拒绝</Button>
        <Button size="small" disabled={!!busy} onClick={() => void decide(student, 'approve')} aria-label={`批准 ${student.name}`}><Check size={15} />批准</Button>
      </div>)}
    </div>
    <ErrorNotice>{error}</ErrorNotice>
  </Dialog>
}

function RoomSettings({ room, refresh, onClose }: { room: TeacherRoom; refresh: () => void; onClose: () => void }) {
  const [name, setName] = useState(room.name)
  const [pin, setPIN] = useState(room.pin ?? '')
  const [sending, setSending] = useState(room.sending)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [saved, setSaved] = useState('')
  const [confirm, setConfirm] = useState(false)
  async function update(regenerate: boolean) {
    setBusy(true)
    setError('')
    setSaved('')
    try {
      const result = await api<{ pin: string }>('teacher', 'settings', { name, pin, sending, regenerate, lessonId: room.lessonId })
      setPIN(result.pin)
      setSaved(regenerate ? '已重新生成口令并保存设置' : '已保存设置')
      refresh()
    } catch (err) { setError(errorMessage(err)) }
    finally { setBusy(false) }
  }
  async function clear() {
    setError('')
    setSaved('')
    setBusy(true)
    try {
      await api('teacher', 'history/clear', { confirm: true, lessonId: room.lessonId })
      setSaved('历史消息已清空')
      setConfirm(false)
      refresh()
    } catch (err) { setError(errorMessage(err)) }
    finally { setBusy(false) }
  }
  return <Dialog title="课堂设置" description="管理课堂名称、口令、学生发送权限和消息记录。" onClose={onClose}>
    <form className="dialog-form" onSubmit={event => { event.preventDefault(); void update(false) }}>
      <label className="field">课堂名称<input required maxLength={40} value={name} onChange={event => setName(event.target.value)} /></label>
      <div className="field"><label htmlFor="classroom-pin">课堂口令</label>
        <span className="input-row"><input id="classroom-pin" inputMode="numeric" pattern="[0-9]{6}" required maxLength={6} value={pin} onChange={event => setPIN(event.target.value.replace(/\D/g, ''))} />
          <Button variant="outline" disabled={busy || !name.trim()} onClick={() => void update(true)}>重新生成并保存</Button></span>
      </div>
      <label className="toggle-row"><span>允许学生发送消息</span><input type="checkbox" role="switch" checked={sending} onChange={event => setSending(event.target.checked)} /></label>
      <div className="dialog-actions"><Button type="submit" disabled={busy}>保存设置</Button></div>
    </form>
    <div className="danger-zone">
      {confirm ? <><p>将永久删除课堂内所有人的历史消息和送达记录。</p><div className="dialog-actions"><Button variant="outline" disabled={busy} onClick={() => setConfirm(false)}>取消</Button>
        <Button variant="danger" disabled={busy} onClick={() => void clear()}>确认清空</Button></div></>
        : <Button variant="ghost" className="danger-text" onClick={() => { setConfirm(true); setSaved('') }}>清空历史消息</Button>}
    </div>
    <ErrorNotice>{error}</ErrorNotice>
    {saved && <p className="success-text" role="status">{saved}</p>}
  </Dialog>
}

function ManageStudent({ student, refresh, onClose }: { student: Student; refresh: () => void; onClose: () => void }) {
  const [name, setName] = useState(student.name)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState(false)
  async function manage(action: string) {
    setBusy(true)
    setError('')
    try { await api('teacher', `students/${student.id}`, { action, name }); refresh(); onClose() }
    catch (err) { setError(errorMessage(err)); refresh() }
    finally { setBusy(false) }
  }
  return <Dialog title="管理学生" description={student.name} onClose={onClose}>
    <form className="dialog-form" onSubmit={event => { event.preventDefault(); void manage('rename') }}>
      <label className="field">显示名称<input required maxLength={40} value={name} onChange={event => setName(event.target.value)} /></label>
      <div className="dialog-actions"><Button type="submit" disabled={busy || !name.trim() || name.trim() === student.name}><Pencil size={14} />保存名称</Button></div>
    </form>
    <div className="danger-zone">{confirm ? <><p>移除后，该学生需要重新申请加入课堂。</p>
      <div className="dialog-actions"><Button variant="outline" onClick={() => setConfirm(false)} disabled={busy}>取消</Button>
        <Button variant="danger" disabled={busy} onClick={() => void manage('remove')}>确认移除</Button></div></>
      : <Button variant="ghost" className="danger-text" onClick={() => setConfirm(true)}><UserMinus size={15} />移除学生</Button>}</div>
    <ErrorNotice>{error}</ErrorNotice>
  </Dialog>
}

export function TeacherPage() {
  const session = useRoom('teacher')
  const { room, connection, error, refresh, revision, retry } = session
  const [modal, setModal] = useState<Modal>(null)
  const [selected, setSelected] = useState('all')
  const [search, setSearch] = useState('')
  const [mobileChat, setMobileChat] = useState(false)
  const [cloudMode, setCloudMode] = useState(false)
  const [logoutError, setLogoutError] = useState('')
  const cloud = room?.cloud ?? cloudMode
  const ready = room && (!room.cloud || room.running)
  useEffect(() => {
    const abort = new AbortController()
    void api<{ cloud: boolean }>('teacher', 'mode', undefined, abort.signal).then(data => {
      if (!abort.signal.aborted) setCloudMode(data.cloud)
    }).catch(() => {})
    return () => abort.abort()
  }, [])

  async function logout() {
    try {
      await api('teacher','logout', {})
      setModal(null)
      setLogoutError('')
      refresh()
    } catch (err) { setLogoutError(errorMessage(err)) }
  }
  const generation = room?.generation
  useEffect(() => () => { if (generation) useApp.getState().forget(`teacher:teacher:${generation}:`) }, [generation])
  function select(id: string) {
    setSelected(id)
    setMobileChat(true)
  }

  const current = room?.students.find(student => student.id === selected)
  const conversation = current?.id ?? 'all'
  const pending = room?.students.filter(student => student.status === 'pending') ?? []
  const active = room?.students.filter(student => student.status === 'approved') ?? []
  const matches = (student: Student) => student.name.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase())
  const online = active.filter(student => student.online)
  const displayOnline = online.filter(matches)
  const displayOffline = active.filter(student => !student.online && matches(student))
  const removed = room?.students.filter(student => student.status === 'removed' && matches(student)) ?? []

  return <div className="teacher-shell">
    <header className="app-topbar">
      <Brand />
      <div className={`topbar-actions ${cloud && room ? 'cloud-toolbar' : ''}`}>
        <ConnectionBadge state={connection} />
        <span className="topbar-divider" />
        <ThemeButton />
        {cloud && room && <>
          <Button variant="ghost" size="small" onClick={() => setModal('history')}>上课记录</Button>
          {room.running
            ? <Button variant="outline" size="small" onClick={() => setModal('end')}>结束课堂</Button>
            : <Button variant="outline" size="small" onClick={() => setModal('invite')}>学生链接</Button>}
          <Button variant="ghost" size="icon" onClick={() => void logout()} aria-label="退出教师账号" title="退出教师账号"><LogOut size={17} /></Button>
        </>}
        {ready && <Button variant="ghost" size="icon" onClick={() => setModal('settings')} aria-label="课堂设置" title="课堂设置"><Settings size={18} /></Button>}
      </div>
    </header>
    <ErrorNotice>{logoutError}</ErrorNotice>
    {room && ready ? <div className={`teacher-workspace ${mobileChat ? 'show-conversation' : ''}`}>
      <aside className="sidebar" aria-label="课堂会话">
        <div className="sidebar-top">
          <div className="class-summary">
            <div><h1 title={room.name}>{room.name}</h1><p>{online.length} 人在线 <span>·</span> {active.length} 位学生</p></div>
            <span className="class-icon"><Users size={19} /></span>
          </div>
          <Button className="invite-button" onClick={() => setModal('invite')}><QrCode size={17} />邀请学生<ChevronRight size={16} className="ml-auto opacity-70" /></Button>
          <button className="approval-button" type="button" onClick={() => setModal('requests')}>
            <UserRoundPlus size={17} />加入申请{pending.length > 0 ? <span className="counter ml-auto">{pending.length}</span> : <ChevronRight size={16} className="ml-auto" />}
          </button>
          <div className="sidebar-line" />
          <button className="broadcast-row" type="button" aria-current={conversation === 'all' ? 'true' : undefined} onClick={() => select('all')}>
            <span className="avatar broadcast-avatar"><Radio size={18} /></span>
            <span><strong>全班广播</strong><small>发送给课堂内所有学生</small></span>
          </button>
          <label className="search"><Search size={16} /><input placeholder="搜索学生" aria-label="搜索学生" value={search} onChange={event => setSearch(event.target.value)} /></label>
        </div>
        <div className="student-list">
          <StudentRows title="在线学生" students={displayOnline} selected={selected} onSelect={select} />
          <StudentRows title="离线学生" students={displayOffline} selected={selected} onSelect={select} />
          <StudentRows title="已移除" students={removed} selected={selected} onSelect={select} />
          {!displayOnline.length && !displayOffline.length && !removed.length && <div className="sidebar-empty"><UserRound size={17} />{search.trim() ? '没有匹配的学生' : '暂无学生，邀请学生加入课堂'}</div>}
        </div>
        <footer className="sidebar-footer"><Network size={15} /><span title={room.url}>{room.running ? room.url.replace(/https?:\/\//, '').replace('/student', '') : '局域网服务已停止'}</span>
          <i className={room.running ? 'lan-on' : ''} /></footer>
      </aside>
      <main className="chat-panel">
        <header className="chat-header">
          <Button className="mobile-back" variant="ghost" size="icon" aria-label="返回会话列表" onClick={() => setMobileChat(false)}><ArrowLeft size={21} /></Button>
          {current ? <Avatar student={current} /> : <span className="avatar broadcast-avatar"><Radio size={21} /></span>}
          <div className="chat-title"><h2>{current?.name ?? '全班广播'}</h2>
            <p>{current ? current.status === 'removed' ? '已移除' : current.online ? '在线' : '离线 · 上线后可接收消息' : `${online.length} 人在线 · ${active.length} 位学生可接收`}</p></div>
          {current?.status === 'approved' && <Button variant="ghost" size="icon" onClick={() => setModal('student')} aria-label="管理学生"><Ellipsis size={22} /></Button>}
        </header>
        {connection === 'offline' && <div className="connection-banner" role="status">连接已断开，正在尝试重新连接。<Button variant="ghost" size="small" onClick={retry}>立即重连</Button></div>}
        <Conversation key={`${room.generation}:${conversation}`} id={conversation} role="teacher" identity="teacher"
          name={current?.name ?? room.name} generation={room.generation} revision={revision} connection={connection}
          disabled={current?.status === 'removed' ? '该学生已移除，保留历史消息。' : undefined} />
      </main>
    </div> : room?.cloud ? <StartLesson refresh={refresh} /> : cloud ? <CloudLogin refresh={refresh} /> : <main className="boot-screen">
      <span className="empty-icon">{error ? <ShieldCheck size={25} /> : <LoaderCircle className="spin" size={25} />}</span>
      <h1>{error ? '进入教师端' : '正在连接课堂'}</h1>
      <ErrorNotice>{error}</ErrorNotice>
      {error && <><p>可以在启动 ClassRelay 的终端找到本次登录链接。</p><Button variant="outline" onClick={retry}>重新连接</Button></>}
    </main>}
    {room && modal === 'invite' && <Invite room={room} refresh={refresh} onClose={() => setModal(null)} />}
    {room && modal === 'requests' && <Requests students={pending} refresh={refresh} onClose={() => setModal(null)} />}
    {room && ready && modal === 'settings' && <RoomSettings room={room} refresh={refresh} onClose={() => setModal(null)} />}
    {ready && current && modal === 'student' && <ManageStudent student={current} refresh={refresh} onClose={() => setModal(null)} />}
    {room && cloud && modal === 'history' && <LessonHistory generation={room.generation} onClose={() => setModal(null)} />}
    {room?.lessonId && modal === 'end' && <EndLesson id={room.lessonId} refresh={refresh} onClose={() => { setModal(null); setMobileChat(false); setSelected('all') }} />}
  </div>
}
