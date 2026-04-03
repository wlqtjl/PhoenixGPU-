// PhoenixGPU WebUI — Alerts page
import { useAlerts, useResolveAlert } from '../api/client'

export default function Alerts() {
  const { data: alerts, isLoading, error } = useAlerts()
  const resolve = useResolveAlert()

  if (isLoading) return <div>Loading alerts...</div>
  if (error) return <div>Error loading alerts</div>

  return (
    <div>
      <h2>Alerts</h2>
      <ul style={{ listStyle: 'none', padding: 0 }}>
        {alerts?.map((a) => (
          <li key={a.id} style={{ padding: '8px 0', borderBottom: '1px solid #eee' }}>
            <span style={{ fontWeight: 'bold', color: a.severity === 'error' ? 'red' : 'orange' }}>
              [{a.severity}]
            </span>{' '}
            {a.message} — {a.tenant}
            {!a.resolved && (
              <button
                onClick={() => resolve.mutate(a.id)}
                style={{ marginLeft: 8 }}
              >
                Resolve
              </button>
            )}
            {a.resolved && <span style={{ color: 'green', marginLeft: 8 }}>✓ Resolved</span>}
          </li>
        ))}
      </ul>
    </div>
  )
}
