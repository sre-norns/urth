import {useState} from 'react'
import {
  Activity,
  Box,
  ChevronLeft,
  ChevronRight,
  CloudCog,
  FlaskConical,
  Menu,
  PlaySquare,
  RadioTower,
  Search,
  ServerCog,
  X,
} from 'lucide-react'
import {NavLink, Outlet, useLocation, useNavigate} from 'react-router-dom'

const navigation = [
  {to: '/scenarios', label: 'Scenarios', icon: FlaskConical},
  {to: '/runs', label: 'Runs', icon: PlaySquare},
  {to: '/runners', label: 'Runners', icon: RadioTower},
  {to: '/workers', label: 'Workers', icon: ServerCog},
  {to: '/artifacts', label: 'Artifacts', icon: Box},
]

export function Layout() {
  const [collapsed, setCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)
  const [globalSearch, setGlobalSearch] = useState('')
  const navigate = useNavigate()
  const location = useLocation()

  function submitSearch(event: React.FormEvent) {
    event.preventDefault()
    const query = globalSearch.trim()
    navigate(query ? `/scenarios?name=${encodeURIComponent(query)}` : '/scenarios')
    setMobileOpen(false)
  }

  return (
    <div className={`app-shell ${collapsed ? 'sidebar-collapsed' : ''}`}>
      <a className="skip-link" href="#main-content">Skip to content</a>
      <aside className={`sidebar ${mobileOpen ? 'sidebar-open' : ''}`}>
        <div className="brand">
          <span className="brand-mark"><Activity size={22} /></span>
          <div className="brand-copy"><strong>URTH</strong><span>synthetic monitoring</span></div>
          <button className="icon-button mobile-close" aria-label="Close menu" onClick={() => setMobileOpen(false)}>
            <X size={20} />
          </button>
        </div>
        <nav aria-label="Primary">
          <span className="nav-section">Monitor</span>
          {navigation.slice(0, 2).map(({to, label, icon: Icon}) => (
            <NavLink key={to} to={to} className={({isActive}) => isActive ? 'nav-link active' : 'nav-link'} onClick={() => setMobileOpen(false)}>
              <Icon size={18} /><span>{label}</span>
            </NavLink>
          ))}
          <span className="nav-section">Infrastructure</span>
          {navigation.slice(2, 4).map(({to, label, icon: Icon}) => (
            <NavLink key={to} to={to} className={({isActive}) => isActive ? 'nav-link active' : 'nav-link'} onClick={() => setMobileOpen(false)}>
              <Icon size={18} /><span>{label}</span>
            </NavLink>
          ))}
          <span className="nav-section">Run data</span>
          {navigation.slice(4).map(({to, label, icon: Icon}) => (
            <NavLink key={to} to={to} className={({isActive}) => isActive ? 'nav-link active' : 'nav-link'} onClick={() => setMobileOpen(false)}>
              <Icon size={18} /><span>{label}</span>
            </NavLink>
          ))}
        </nav>
        <div className="sidebar-foot">
          <div className="api-status"><span /><div><strong>API connected</strong><small>Live operational data</small></div></div>
          <button className="collapse-button" onClick={() => setCollapsed((value) => !value)} aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}>
            {collapsed ? <ChevronRight size={17} /> : <ChevronLeft size={17} />}
            <span>Collapse</span>
          </button>
        </div>
      </aside>
      {mobileOpen && <button className="mobile-scrim" aria-label="Close menu" onClick={() => setMobileOpen(false)} />}
      <div className="main-column">
        <header className="topbar">
          <button className="icon-button mobile-menu" aria-label="Open menu" onClick={() => setMobileOpen(true)}>
            <Menu size={20} />
          </button>
          <form className="global-search" onSubmit={submitSearch}>
            <Search size={16} aria-hidden="true" />
            <input aria-label="Search scenarios" placeholder="Search scenarios…" value={globalSearch} onChange={(event) => setGlobalSearch(event.target.value)} />
            <kbd>/</kbd>
          </form>
          <div className="top-context">
            <CloudCog size={17} />
            <span>{location.pathname.split('/')[1] || 'scenarios'}</span>
          </div>
        </header>
        <main id="main-content" tabIndex={-1}>
          <Outlet />
        </main>
      </div>
    </div>
  )
}
