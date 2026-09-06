import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, CartesianGrid, ResponsiveContainer,
} from 'recharts'
import {
  getMcpStatsSummary,
  getMcpStatsTrend,
  getMcpAdminInfo,
} from '../api/client'
import type {
  McpOverviewStats,
  McpSummaryRow,
  McpTrendRow,
  McpAdminInfo,
} from '../api/client'

function fmtTs(ts: number, bucket: string) {
  if (bucket === 'minute') {
    return new Date(ts * 1000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  }
  const d = new Date(ts * 1000)
  const now = new Date()
  if (now.toDateString() === d.toDateString()) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function fmtNum(n: number | null | undefined): string {
  if (n == null) return '—'
  return n >= 1000 ? (n / 1000).toFixed(1) + 'k' : String(n)
}

function RefreshIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8">
      <path d="M2 8a6 6 0 1 1 1.5 4"/>
      <polyline points="2 12 2 8 6 8"/>
    </svg>
  )
}

type TimeRange = '4h' | '8h' | '12h' | 'today' | '3d' | '7d' | 'all'

const TIME_RANGE_OPTIONS: { value: TimeRange; label: string }[] = [
  { value: '4h',    label: '最近 4 小时' },
  { value: '8h',    label: '最近 8 小时' },
  { value: '12h',   label: '最近 12 小时' },
  { value: 'today', label: '今天' },
  { value: '3d',    label: '最近 3 天' },
  { value: '7d',    label: '最近 7 天' },
  { value: 'all',   label: '所有' },
]

function toSince(range: TimeRange): number | undefined {
  const now = Date.now() / 1000
  if (range === '4h')    return now - 4 * 3600
  if (range === '8h')    return now - 8 * 3600
  if (range === '12h')   return now - 12 * 3600
  if (range === 'today') {
    const d = new Date(); d.setHours(0, 0, 0, 0)
    return d.getTime() / 1000
  }
  if (range === '3d')  return now - 3 * 86400
  if (range === '7d')  return now - 7 * 86400
  return undefined
}

function trendBucket(range: TimeRange): string {
  if (range === '4h' || range === '8h' || range === '12h') return 'minute'
  return 'hour'
}

export default function McpOverview() {
  const [overview, setOverview] = useState<McpOverviewStats | null>(null)
  const [summary, setSummary] = useState<McpSummaryRow[]>([])
  const [trend, setTrend] = useState<McpTrendRow[]>([])
  const [info, setInfo] = useState<McpAdminInfo | null>(null)
  const [groupBy, setGroupBy] = useState('combo')
  const [timeRange, setTimeRange] = useState<TimeRange>('today')
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(true)

  const refresh = async (range = timeRange) => {
    setErr('')
    const since = toSince(range)
    try {
      const [sumRes, trendRes, infoRes] = await Promise.all([
        getMcpStatsSummary({ group_by: groupBy, since }),
        getMcpStatsTrend({ bucket: trendBucket(range), since }),
        getMcpAdminInfo().catch(() => null),
      ])
      setOverview(sumRes.overview)
      setSummary(sumRes.data)
      setTrend(trendRes.data)
      if (infoRes) setInfo(infoRes)
    } catch (e: unknown) {
      setErr(String(e))
    }
    setLoading(false)
  }

  useEffect(() => {
    refresh()
  }, [groupBy, timeRange])

  const totalCalls = overview?.total_calls ?? 0
  const successCalls = overview?.success_calls ?? 0
  const errorCalls = overview?.error_calls ?? 0
  const avgDur = overview?.avg_duration_ms ? Math.round(overview.avg_duration_ms) : null

  if (loading) return <div className="page"><div className="empty-state">加载中…</div></div>

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <span className="page-title">概览</span>
          <span className="page-sub" style={{ marginLeft: 12 }}>MCP 网关运行状态与工具调用大盘</span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <select
            value={timeRange}
            onChange={e => setTimeRange(e.target.value as TimeRange)}
            style={{ width: 'auto', minWidth: 130 }}
          >
            {TIME_RANGE_OPTIONS.map(o => (
              <option key={o.value} value={o.value}>{o.label}</option>
            ))}
          </select>
          <button className="refresh-btn" onClick={() => refresh()}>
            <RefreshIcon />刷新
          </button>
        </div>
      </div>

      {err && <div className="alert err">{err}</div>}

      {/* Stat Cards */}
      <div className="stat-grid">
        <div className="stat-card">
          <div className="stat-label">总调用数</div>
          <div className="stat-value">{fmtNum(totalCalls)}</div>
          <div className="stat-sub">
            <span className="badge ok">{fmtNum(successCalls)} 成功</span>
            {errorCalls > 0 && (
              <span className="badge err" style={{ marginLeft: 4 }}>{fmtNum(errorCalls)} 失败</span>
            )}
          </div>
        </div>

        <div className="stat-card">
          <div className="stat-label">生效工具总量</div>
          <div className="stat-value" style={{ color: 'var(--accent)' }}>
            {fmtNum(info?.total_tools ?? overview?.unique_tools ?? 0)}
          </div>
          <div className="stat-sub">本周期调用 {fmtNum(overview?.unique_tools ?? 0)} 种不同工具</div>
        </div>

        <div className="stat-card">
          <div className="stat-label">平均响应耗时</div>
          <div className="stat-value">{avgDur != null ? avgDur + ' ms' : '—'}</div>
          <div className="stat-sub">端到端工具执行时间</div>
        </div>

        <div className="stat-card">
          <div className="stat-label">活跃 Providers</div>
          <div className="stat-value">{overview?.active_providers ?? 0}</div>
          <div className="stat-sub">共配置 {info?.providers.length ?? 0} 个上游</div>
        </div>

        <div className="stat-card">
          <div className="stat-label">聚合 Combos</div>
          <div className="stat-value">{overview?.active_combos ?? 0}</div>
          <div className="stat-sub">共配置 {info?.combos.length ?? 0} 个工具箱</div>
        </div>

        <div className="stat-card">
          <div className="stat-label">活跃隔离用户</div>
          <div className="stat-value" style={{ color: 'var(--accent)' }}>
            {fmtNum(overview?.active_users ?? 0)}
          </div>
          <div className="stat-sub">已存凭据 {info?.tokens_count ?? 0} 个令牌</div>
        </div>
      </div>

      {/* Trend Chart */}
      <div className="chart-wrap" style={{ marginBottom: 24 }}>
        <div className="chart-head">
          <span className="chart-title">工具调用趋势（{trendBucket(timeRange) === 'minute' ? '按分钟' : '按小时'}）</span>
        </div>
        {trend.length === 0
          ? <div className="empty-state" style={{ padding: 32 }}>当前周期内暂无调用数据</div>
          : (
            <ResponsiveContainer width="100%" height={220}>
              <BarChart data={trend} margin={{ top: 4, right: 8, left: -20, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#e8e4de" vertical={false} />
                <XAxis
                  dataKey="bucket_ts"
                  tickFormatter={v => fmtTs(Number(v), trendBucket(timeRange))}
                  tick={{ fontSize: 11, fill: '#9e9b96' }}
                  axisLine={false}
                  tickLine={false}
                />
                <YAxis tick={{ fontSize: 11, fill: '#9e9b96' }} axisLine={false} tickLine={false} />
                <Tooltip
                  labelFormatter={v => fmtTs(Number(v), trendBucket(timeRange))}
                  contentStyle={{ fontSize: 12, borderRadius: 6, border: '1px solid #e2ded8', background: '#fff' }}
                  cursor={{ fill: 'rgba(0,0,0,0.03)' }}
                />
                <Bar dataKey="success_count" name="成功" fill="#1a5c3a" radius={[3, 3, 0, 0]} stackId="a" />
                <Bar dataKey={(r: McpTrendRow) => r.total - r.success_count} name="失败" fill="#f87171" radius={[3, 3, 0, 0]} stackId="a" />
              </BarChart>
            </ResponsiveContainer>
          )}
      </div>

      {/* Summary Table */}
      <div style={{ marginBottom: 28 }}>
        <div className="toolbar" style={{ marginBottom: 12, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <h2>聚合统计</h2>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ fontSize: 12, color: 'var(--text-3)' }}>分组维度:</span>
            <select
              value={groupBy}
              onChange={e => setGroupBy(e.target.value)}
              style={{ width: 'auto', minWidth: 120 }}
            >
              <option value="combo">按 Combo</option>
              <option value="provider">按 Provider</option>
              <option value="method">按 Method</option>
              <option value="tool">按 工具名称</option>
              <option value="transport">按 传输协议</option>
            </select>
          </div>
        </div>

        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>分组标识</th>
                <th>调用总计</th>
                <th>成功数</th>
                <th>失败数</th>
                <th>成功率</th>
                <th>平均耗时</th>
                <th>最短耗时</th>
                <th>最长耗时</th>
              </tr>
            </thead>
            <tbody>
              {summary.length === 0 && (
                <tr><td colSpan={8} style={{ textAlign: 'center', color: 'var(--text-3)', padding: 24 }}>暂无统计数据</td></tr>
              )}
              {summary.map((row, i) => {
                const rate = row.total > 0 ? Math.round((row.success_count / row.total) * 100) : 0
                return (
                  <tr key={i}>
                    <td><span className="mono">{row.group_key ?? <em className="text-muted">未知</em>}</span></td>
                    <td>{row.total}</td>
                    <td style={{ color: 'var(--ok-fg)', fontWeight: 600 }}>{row.success_count}</td>
                    <td style={{ color: row.error_count > 0 ? 'var(--err-fg)' : 'var(--text-3)' }}>
                      {row.error_count}
                    </td>
                    <td>
                      <span className={`badge ${rate >= 95 ? 'ok' : rate >= 80 ? 'warn' : 'err'}`}>
                        {rate}%
                      </span>
                    </td>
                    <td>{row.avg_duration_ms > 0 ? Math.round(row.avg_duration_ms) + ' ms' : '—'}</td>
                    <td style={{ color: 'var(--text-3)', fontSize: 12 }}>
                      {row.min_duration_ms != null && row.min_duration_ms > 0 ? row.min_duration_ms + ' ms' : '—'}
                    </td>
                    <td style={{ color: 'var(--text-3)', fontSize: 12 }}>
                      {row.max_duration_ms != null && row.max_duration_ms > 0 ? row.max_duration_ms + ' ms' : '—'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </div>

      {/* Upstream Provider Pool Status */}
      <div className="toolbar" style={{ marginBottom: 12, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <h2>上游 Providers 状态</h2>
        <Link to="/mcp/info" style={{ fontSize: 12, color: 'var(--accent)', textDecoration: 'none' }}>
          查看完整网关接入配置 ➜
        </Link>
      </div>

      {(!info || info.providers.length === 0) ? (
        <div className="empty-state">暂无已配置的 MCP Provider</div>
      ) : (
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: 14 }}>
          {info.providers.map(p => {
            const tokenCount = info.provider_tokens?.[p.name] ?? 0
            const transportColor = p.transport === 'stdio' ? '#8b5cf6' : p.transport === 'sse' ? '#0284c7' : '#10b981'
            return (
              <div
                key={p.name}
                style={{
                  background: 'var(--bg-panel)',
                  border: '1px solid var(--border)',
                  borderRadius: 8,
                  padding: 16,
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 10,
                }}
              >
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                  <span style={{ fontWeight: 600, fontSize: 14 }}>{p.name}</span>
                  <span
                    style={{
                      fontSize: 11,
                      fontWeight: 600,
                      padding: '2px 8px',
                      borderRadius: 12,
                      color: transportColor,
                      background: `${transportColor}18`,
                    }}
                  >
                    {p.transport.toUpperCase()}
                  </span>
                </div>

                <div style={{ fontSize: 12, color: 'var(--text-3)', wordBreak: 'break-all', fontFamily: 'var(--font-mono)' }}>
                  {p.transport === 'stdio' ? (p.command || 'stdio process') : (p.url || '—')}
                </div>

                <div style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 11, color: 'var(--text-2)', marginTop: 'auto', paddingTop: 8, borderTop: '1px solid var(--border-light)' }}>
                  <span>鉴权: <code>{p.auth_mode ?? 'shared'}</code></span>
                  {tokenCount > 0 && (
                    <span className="badge ok" style={{ fontSize: 10 }}>{tokenCount} 个用户令牌</span>
                  )}
                  <Link
                    to="/mcp/test"
                    style={{ marginLeft: 'auto', color: 'var(--accent)', textDecoration: 'none', fontSize: 11, fontWeight: 500 }}
                  >
                    测试 ➜
                  </Link>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
