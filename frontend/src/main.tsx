import { createRoot } from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router'
import { StudentPage } from './pages/student'
import { TeacherPage } from './pages/teacher'
import './styles.css'

createRoot(document.getElementById('root')!).render(<BrowserRouter>
  <Routes>
    <Route path="/teacher" element={<TeacherPage />} />
    <Route path="/student" element={<StudentPage />} />
    <Route path="*" element={<Navigate to="/teacher" replace />} />
  </Routes>
</BrowserRouter>)
