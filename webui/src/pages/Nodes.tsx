// PhoenixGPU WebUI — GPU Nodes page
import { useNodes } from '../api/client'

export default function Nodes() {
  const { data: nodes, isLoading, error } = useNodes()

  if (isLoading) return <div>Loading nodes...</div>
  if (error) return <div>Error loading nodes</div>

  return (
    <div>
      <h2>GPU Nodes</h2>
      <table style={{ width: '100%', borderCollapse: 'collapse' }}>
        <thead>
          <tr>
            <th>Name</th>
            <th>GPU Model</th>
            <th>Count</th>
            <th>VRAM Used</th>
            <th>SM Util %</th>
            <th>Temp °C</th>
            <th>Power W</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {nodes?.map((n) => (
            <tr key={n.name}>
              <td>{n.name}</td>
              <td>{n.gpuModel}</td>
              <td>{n.gpuCount}</td>
              <td>{n.vramUsedMiB}/{n.vramTotalMiB} MiB</td>
              <td>{n.smUtilPct}%</td>
              <td>{n.tempCelsius}</td>
              <td>{n.powerWatt}</td>
              <td>{n.ready ? '✅ Ready' : '❌ NotReady'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
