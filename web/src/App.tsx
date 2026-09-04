import { useState, useEffect } from 'react'
import { NavLink, Route, Routes } from 'react-router-dom'
import './App.css'
import Overview from './pages/Overview'
import Requests from './pages/Requests'
import ConfigEditor from './pages/ConfigEditor'
import TestPage from './pages/Test'
import InfoPage from './pages/Info'
import LogsPage from './pages/Logs'
import Login from './components/Login'
import { fetchAuthStatus, logoutAdmin } from './api/client'

function IconLogout() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
      <polyline points="16 17 21 12 16 7" />
      <line x1="21" y1="12" x2="9" y2="12" />
    </svg>
  )
}

function IconChart() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <rect x="1" y="9" width="3" height="6" rx="0.5"/>
      <rect x="6" y="5" width="3" height="10" rx="0.5"/>
      <rect x="11" y="2" width="3" height="13" rx="0.5"/>
    </svg>
  )
}

function IconList() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <line x1="3" y1="4" x2="13" y2="4"/>
      <line x1="3" y1="8" x2="13" y2="8"/>
      <line x1="3" y1="12" x2="9" y2="12"/>
    </svg>
  )
}

function IconSettings() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <circle cx="8" cy="8" r="2.5"/>
      <path d="M8 1v2M8 13v2M1 8h2M13 8h2M3.05 3.05l1.41 1.41M11.54 11.54l1.41 1.41M3.05 12.95l1.41-1.41M11.54 4.46l1.41-1.41"/>
    </svg>
  )
}

function IconFlask() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <path d="M6 2v5L2 13a1 1 0 0 0 .9 1.5h10.2A1 1 0 0 0 14 13L10 7V2"/>
      <line x1="5" y1="2" x2="11" y2="2"/>
    </svg>
  )
}

function IconInfo() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <circle cx="8" cy="8" r="6.5"/>
      <line x1="8" y1="7" x2="8" y2="11"/>
      <circle cx="8" cy="5" r="0.5" fill="currentColor" stroke="none"/>
    </svg>
  )
}

function IconLog() {
  return (
    <svg className="nav-icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <rect x="2" y="1.5" width="12" height="13" rx="1.5"/>
      <line x1="5" y1="5" x2="11" y2="5"/>
      <line x1="5" y1="8" x2="11" y2="8"/>
      <line x1="5" y1="11" x2="8.5" y2="11"/>
    </svg>
  )
}

function IconSidebarCollapse() {
  return (
    <svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <rect x="2" y="2" width="12" height="12" rx="1.5"/>
      <line x1="6" y1="2" x2="6" y2="14"/>
      <polyline points="11 6 9 8 11 10"/>
    </svg>
  )
}

function IconSidebarExpand() {
  return (
    <svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <rect x="2" y="2" width="12" height="12" rx="1.5"/>
      <line x1="6" y1="2" x2="6" y2="14"/>
      <polyline points="9 6 11 8 9 10"/>
    </svg>
  )
}

export default function App() {
  const [authRequired, setAuthRequired] = useState(false)
  const [loggedIn, setLoggedIn] = useState(true)
  const [authChecking, setAuthChecking] = useState(true)

  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem('ccrouter_sidebar_collapsed') === 'true'
    } catch {
      return false
    }
  })

  useEffect(() => {
    let mounted = true
    const checkAuth = async () => {
      try {
        const res = await fetchAuthStatus()
        if (mounted) {
          setAuthRequired(res.auth_required)
          setLoggedIn(res.logged_in)
        }
      } catch (err) {
        console.error('Failed to check auth status', err)
      } finally {
        if (mounted) {
          setAuthChecking(false)
        }
      }
    }
    checkAuth()

    const onUnauthorized = () => {
      setLoggedIn(false)
    }
    window.addEventListener('ccrouter:unauthorized', onUnauthorized)
    return () => {
      mounted = false
      window.removeEventListener('ccrouter:unauthorized', onUnauthorized)
    }
  }, [])

  const handleLogout = async () => {
    await logoutAdmin()
    setLoggedIn(false)
  }

  const toggleCollapsed = () => {
    setCollapsed(prev => {
      const next = !prev
      try {
        localStorage.setItem('ccrouter_sidebar_collapsed', String(next))
      } catch {}
      return next
    })
  }

  if (authChecking) {
    return null
  }

  if (authRequired && !loggedIn) {
    return <Login onLoginSuccess={() => setLoggedIn(true)} />
  }

  return (
    <div className="layout">
      <nav className={`sidebar ${collapsed ? 'collapsed' : ''}`}>
        <div className="brand">
          <div className="brand-text">
            <div className="brand-name">ccrouter</div>
            <div className="brand-sub">Admin</div>
          </div>
          <button
            className="sidebar-toggle-btn"
            onClick={toggleCollapsed}
            title={collapsed ? '展开菜单' : '收起菜单'}
            aria-label={collapsed ? '展开菜单' : '收起菜单'}
          >
            {collapsed ? <IconSidebarExpand /> : <IconSidebarCollapse />}
          </button>
        </div>
        <div className="nav-section">
          <div className="nav-label">监控</div>
          <NavLink to="/" end className={({ isActive }) => isActive ? 'nav-link active' : 'nav-link'} title={collapsed ? '概览' : undefined}>
            <IconChart /><span>概览</span>
          </NavLink>
          <NavLink to="/requests" className={({ isActive }) => isActive ? 'nav-link active' : 'nav-link'} title={collapsed ? '请求明细' : undefined}>
            <IconList /><span>请求明细</span>
          </NavLink>
          <NavLink to="/logs" className={({ isActive }) => isActive ? 'nav-link active' : 'nav-link'} title={collapsed ? '日志' : undefined}>
            <IconLog /><span>日志</span>
          </NavLink>
          <div className="nav-label" style={{ marginTop: 16 }}>管理</div>
          <NavLink to="/config" className={({ isActive }) => isActive ? 'nav-link active' : 'nav-link'} title={collapsed ? '配置' : undefined}>
            <IconSettings /><span>配置</span>
          </NavLink>
          <NavLink to="/test" className={({ isActive }) => isActive ? 'nav-link active' : 'nav-link'} title={collapsed ? '测试' : undefined}>
            <IconFlask /><span>测试</span>
          </NavLink>
          <NavLink to="/info" className={({ isActive }) => isActive ? 'nav-link active' : 'nav-link'} title={collapsed ? '信息' : undefined}>
            <IconInfo /><span>信息</span>
          </NavLink>
        </div>
        <div className="sidebar-footer">
          <div className="footer-meta">
            <span>ccrouter</span>
            {!collapsed && <span style={{ opacity: 0.5 }}>v0.6.1</span>}
          </div>
          {authRequired && (
            <button
              className="sidebar-logout-btn"
              onClick={handleLogout}
              title={collapsed ? '退出登录' : undefined}
            >
              <IconLogout />
              <span>退出登录</span>
            </button>
          )}
        </div>
      </nav>
      <main className="content">
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/requests" element={<Requests />} />
          <Route path="/config" element={<ConfigEditor />} />
          <Route path="/test" element={<TestPage />} />
          <Route path="/info" element={<InfoPage />} />
          <Route path="/logs" element={<LogsPage />} />
        </Routes>
      </main>
    </div>
  )
}

