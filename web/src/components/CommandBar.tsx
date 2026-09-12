import { Link, useLocation } from 'react-router-dom'
import type { ReactNode } from 'react'

export function CommandBar({ children }: { children?: ReactNode }) {
  const { pathname } = useLocation()
  const on = (p: string) => (p === '/' ? pathname === '/' : pathname.startsWith(p))
  return (
    <header className="cmdbar">
      <Link to="/" className="brand">
        <span className="mark">BREAK<b>OUT</b></span>
        <span className="sub hide-sm">classroom traffic control</span>
      </Link>
      <nav style={{ display: 'flex', gap: 2 }}>
        <Link className={`nav-link${on('/') ? ' on' : ''}`} to="/">Dashboard</Link>
        <Link className={`nav-link${on('/setup') ? ' on' : ''}`} to="/setup">Setup</Link>
        <Link className={`nav-link${on('/join') ? ' on' : ''}`} to="/join">Join</Link>
      </nav>
      {children}
    </header>
  )
}
