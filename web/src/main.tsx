import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { routerBasename } from './lib/api'
import { App } from './App'
import './styles.css'

// index.html is part of the scaffold and carries no icon link, so the browser
// asks the origin root for /favicon.ico and logs a 404 on every page load.
// Registering one here keeps that out of the console without editing the
// scaffold, and gives the tab an actual mark.
const favicon = document.createElement('link')
favicon.rel = 'icon'
favicon.type = 'image/svg+xml'
favicon.href =
  'data:image/svg+xml,' +
  encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
       <rect width="32" height="32" rx="5" fill="#0b0e14"/>
       <rect x="6"  y="19" width="5" height="7"  rx="1" fill="#ff6b4a"/>
       <rect x="13.5" y="13" width="5" height="13" rx="1" fill="#f2a63b"/>
       <rect x="21" y="7"  width="5" height="19" rx="1" fill="#4fc7e8"/>
     </svg>`,
  )
document.head.appendChild(favicon)

const root = document.getElementById('root')
if (!root) throw new Error('#root is missing from index.html')

createRoot(root).render(
  <StrictMode>
    {/* The basename comes from the same source as the API prefix: Vite's
        BASE_URL. Hardcoding either one is how this app 404s in production. */}
    <BrowserRouter basename={routerBasename}>
      <App />
    </BrowserRouter>
  </StrictMode>,
)
