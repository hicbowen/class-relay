export type Role = 'teacher' | 'student'
export type Mode = 'text' | 'code'
export type Connection = 'connecting' | 'online' | 'offline'
export type Student = {
  id: string
  name: string
  status: 'pending' | 'approved' | 'denied' | 'removed'
  lastSeen: number
  unread: number
  online: boolean
}
export type Room = { name: string; sending: boolean; generation: number; cloud?: boolean; lessonId?: string }
export type TeacherRoom = Room & {
  pin?: string
  addresses: { ip: string; name: string }[]
  students: Student[]
  ip?: string
  running: boolean
  url: string
  networkError?: string
}
export type StudentRoom = Room & { student: Student | null }
export type Lesson = { id: string; name: string; createdAt: number }
export type ArchivedLesson = Lesson & { students: Student[] }
export type Message = {
  seq: number
  id: string
  classroomId: string
  senderId: string
  receiverId: string
  type: Mode
  content: string
  language?: string
  createdAt: number
  requestId?: string
  delivered: number
  recipients: number
}
export type Receipt = Pick<Message, 'id' | 'delivered' | 'recipients'>
export type Page = { messages: Message[]; more: boolean; generation: number }
export type Draft = {
  content: string
  type: Mode
  language: string
}
export type Submission = Draft & {
  requestId: string
  receiverId: string
  studentId?: string
  generation: number
}
export const languages = [
  ['python', 'Python'], ['cpp', 'C++'], ['c', 'C'], ['javascript', 'JavaScript'],
  ['typescript', 'TypeScript'], ['go', 'Go'], ['java', 'Java'], ['html', 'HTML'],
  ['css', 'CSS'], ['json', 'JSON'], ['bash', 'Bash'], ['sql', 'SQL'], ['plaintext', '纯文本'],
] as const
