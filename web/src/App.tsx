import { Navigate, Route, Routes } from 'react-router-dom'
import { Dashboard } from './pages/Dashboard'
import { Join } from './pages/Join'
import { Setup } from './pages/Setup'

export function App() {
  return (
    <Routes>
      <Route path="/" element={<Dashboard />} />
      <Route path="/setup" element={<Setup />} />
      <Route path="/join" element={<Join />} />
      <Route path="/join/:sessionId" element={<Join />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
