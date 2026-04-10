// PhoenixGPU WebUI — App Shell
// Copyright 2025 PhoenixGPU Authors
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Routes, Route, NavLink, Navigate, useLocation } from 'react-router-dom'
import { Suspense, lazy } from 'react'
import '../styles/design-system.css'
import styles from './App.module.css'
import { useAlerts, useClusterSummary } from '../api/client'

const Dashboard      = lazy(() => import('../pages/Dashboard'))
const Nodes          = lazy(() => import('../pages/Nodes'))
const NodeDetail     = lazy(() => import('../pages/NodeDetail'))
const Jobs           = lazy(() => import('../pages/Jobs'))
const JobDetail      = lazy(() => import('../pages/JobDetail'))
const Billing        = lazy(() => import('../pages/Billing'))
const BillingRecords = lazy(() => import('../pages/BillingRecords'))
const AlertsPage     = lazy(() => import('../pages/Alerts'))
const Settings       = lazy(() => import('../pages/Settings'))

const qc = new QueryClient({
  defaultOptions: { queries: { retry: 2, refetchOnWindowFocus: false } },
})

export default function App() {
  return (
    <QueryClientProvider client={qc}>
      <BrowserRouter>
        <AppShell />
      </BrowserRouter>
    </QueryClientProvider>
  )
}

function AppShell() {
  const { data: summary } = useClusterSummary()
  const { data: alerts }  = useAlerts()
  const alertCount = alerts?.filter(a => !a.resolved).length ?? 0

  return (
    <div className={styles.app}>
      <aside className={styles.sidebar}>
        <div className={styles.logo}>
          <div className={styles.logoIcon}>Px</div>
          <div>
            <div className={styles.logoText}>PhoenixGPU</div>
            <div className={styles.logoVer}>v0.1.0-dev</div>
          </div>
        </div>

        <nav className={styles.nav}>
          <div className={styles.navSection}>Monitor</div>
          <NavLink to="/dashboard" className={navCls}><NavDot />Dashboard</NavLink>
          <NavLink to="/nodes"     className={navCls}><NavDot />GPU Nodes</NavLink>

          <div className={styles.navSection}>Workloads</div>
          <NavLink to="/jobs"   className={navCls}>
            <NavDot />PhoenixJobs
            {summary && <span className={styles.navBadge}>{summary.activeJobs}</span>}
          </NavLink>
          <NavLink to="/alerts" className={navCls}>
            <NavDot />Alerts
            {alertCount > 0 && <span className={`${styles.navBadge} ${styles.navBadgeRed}`}>{alertCount}</span>}
          </NavLink>

          <div className={styles.navSection}>Finance</div>
          <NavLink to="/billing" className={navCls}><NavDot />Billing Center</NavLink>

          <div className={styles.navSection}>System</div>
          <NavLink to="/settings" className={navCls}><NavDot />Settings</NavLink>
        </nav>

        <div className={styles.sidebarFooter}>
          <div className={styles.clusterStatus}>
            <div className={styles.statusDot} />
            Cluster healthy · {summary?.totalGPUs ?? '—'} GPUs
          </div>
        </div>
      </aside>

      <header className={styles.header}>
        <PageTitle />
        <span className={styles.headerMeta}>
          {summary ? `Util: ${summary.avgUtilPct}% · Jobs: ${summary.activeJobs}` : 'Loading...'}
        </span>
      </header>

      <main className={styles.main}>
        <Suspense fallback={<div className={styles.loading}>Loading...</div>}>
          <Routes>
            <Route path="/"                        element={<Navigate to="/dashboard" replace />} />
            <Route path="/dashboard"               element={<Dashboard />} />
            <Route path="/nodes"                   element={<Nodes />} />
            <Route path="/nodes/:name"             element={<NodeDetail />} />
            <Route path="/jobs"                    element={<Jobs />} />
            <Route path="/jobs/:namespace/:name"   element={<JobDetail />} />
            <Route path="/alerts"                  element={<AlertsPage />} />
            <Route path="/billing"                 element={<Billing />} />
            <Route path="/billing/records"         element={<BillingRecords />} />
            <Route path="/billing/records/:department" element={<BillingRecords />} />
            <Route path="/settings"                element={<Settings />} />
          </Routes>
        </Suspense>
      </main>
    </div>
  )
}

const navCls = ({ isActive }: { isActive: boolean }) =>
  `nav-item${isActive ? ' active' : ''}`

function NavDot() {
  return <div style={{ width: 6, height: 6, borderRadius: '50%', background: 'currentColor', opacity: 0.5 }} />
}

function PageTitle() {
  const location = useLocation()
  const path = location.pathname
  const titles: Record<string, string> = {
    '/dashboard': 'Dashboard',
    '/nodes':     'GPU Nodes',
    '/jobs':      'PhoenixJobs',
    '/alerts':    'Alerts',
    '/billing':   'Billing Center',
    '/settings':  'Settings',
  }
  // Handle sub-pages
  if (path.startsWith('/nodes/'))               return <div className={styles.pageTitle}>Node Detail</div>
  if (path.startsWith('/jobs/'))                return <div className={styles.pageTitle}>Job Detail</div>
  if (path.startsWith('/billing/records'))      return <div className={styles.pageTitle}>Usage Records</div>
  return <div className={styles.pageTitle}>{titles[path] ?? 'PhoenixGPU'}</div>
}
