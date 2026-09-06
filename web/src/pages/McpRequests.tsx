import { useEffect, useRef, useState } from 'react'
import { getMcpRequests } from '../api/client'
import type { McpRequestRow } from '../api/client'

const PAGE_SIZE = 30

const METHOD_COLOR: Record<string, { fg: string; bg: string }> = {
  'tools/call': { fg: 'var(--accent)', bg: 'var(--accent-light)' },
  'tools/list': { fg: '#0284c7', bg: 'rgba(2, 132, 199, 0.1)' },
  'resources/read': { fg: '#10b981', bg: 'rgba(16, 185, 129, 0.1)' },
  'resources/list': { fg: '#059669', bg: 'rgba(5, 150, 105, 0.1)' },
  'prompts/get': { fg: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.1)' },
  'prompts/list': { fg: '#7c3aed', bg: 'rgba(124, 58, 237, 0.1)' },
  'initialize': { fg: '#f59e0b', bg: 'rgba(245, 158, 11, 0.1)' },
}

export default function McpRequests() {
  const [rows, setRows] = useState<McpRequestRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [combo, setCombo] = useState('')
  const [provider, setProvider] = useState('')
  const [tool, setTool] = useState('')
  const [userID, setUserID] = useState('')
  const [method, setMethod] = useState('')
  const [successFilter, setSuccessFilter] = useState<'' | 'true' | 'false'>('')
  const [err, setErr] = useState('')
  const [autoRefresh, setAutoRefresh] = useState(false)

  // Selected row for detail modal
  const [selectedRow, setSelectedRow] = useState<McpRequestRow | null>(null)
  const [copiedKey, setCopiedKey] = useState<string | null>(null)

  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const load = async () => {
    try {
      const res = await getMcpRequests({
        limit: PAGE_SIZE,
        offset: page * PAGE_SIZE,
        combo: combo || undefined,
        provider: provider || undefined,
        tool: tool || undefined,
        user_id: userID || undefined,
        method: method || undefined,
        success: successFilter === '' ? undefined : successFilter === 'true',
      })
      setRows(res.items)
      setTotal(res.total)
      setErr('')
    } catch (e: unknown) {
      setErr(String(e))
    }
  }

  useEffect(() => {
    load()
  }, [page, combo, provider, tool, userID, method, successFilter])

  useEffect(() => {
    if (autoRefresh) {
      timerRef.current = setInterval(load, 5000)
    } else if (timerRef.current) {
      clearInterval(timerRef.current)
    }
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [autoRefresh, page, combo, provider, tool, userID, method, successFilter])

  const totalPages = Math.ceil(total / PAGE_SIZE)

  const copyText = (key: string, text: string) => {
    navigator.clipboard.writeText(text)
    setCopiedKey(key)
    setTimeout(() => setCopiedKey(null), 2000)
  }

  const formatJSON = (raw: string | null) => {
    if (!raw) return '-'
    try {
      const parsed = JSON.parse(raw)
      return JSON.stringify(parsed, null, 2)
    } catch {
      return raw
    }
  }

  const formatTime = (ts: number) => {
    const d = new Date(ts * 1000)
    const pad = (n: number) => String(n).padStart(2, '0')
    const ms = String(Math.floor((ts % 1) * 1000)).padStart(3, '0')
    return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${ms}`
  }

  const formatFullDate = (ts: number) => {
    return new Date(ts * 1000).toLocaleString()
  }

  return (
    <div className="page">
      <div className="page-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <span className="page-title">MCP 请求明细</span>
          <span className="page-sub">共 {total} 条调用审计记录</span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <label style={{ fontSize: 12, display: 'flex', alignItems: 'center', gap: 5, color: 'var(--text-3)', cursor: 'pointer' }}>
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={e => setAutoRefresh(e.target.checked)}
            />
            自动刷新 (5s)
          </label>
          <button className="btn btn-sm" onClick={() => load()}>
            🔄 刷新
          </button>
        </div>
      </div>

      {err && <div className="alert err">{err}</div>}

      {/* Filters */}
      <div className="filters" style={{ flexWrap: 'wrap', gap: 8 }}>
        <input
          placeholder="Combo"
          value={combo}
          onChange={e => { setCombo(e.target.value); setPage(0) }}
          style={{ minWidth: 100 }}
        />
        <input
          placeholder="Provider"
          value={provider}
          onChange={e => { setProvider(e.target.value); setPage(0) }}
          style={{ minWidth: 100 }}
        />
        <input
          placeholder="Tool / 目标"
          value={tool}
          onChange={e => { setTool(e.target.value); setPage(0) }}
          style={{ minWidth: 120 }}
        />
        <input
          placeholder="User ID"
          value={userID}
          onChange={e => { setUserID(e.target.value); setPage(0) }}
          style={{ minWidth: 100 }}
        />
        <select
          value={method}
          onChange={e => { setMethod(e.target.value); setPage(0) }}
          style={{ minWidth: 120 }}
        >
          <option value="">全部方法</option>
          <option value="tools/call">tools/call</option>
          <option value="tools/list">tools/list</option>
          <option value="resources/read">resources/read</option>
          <option value="resources/list">resources/list</option>
          <option value="prompts/get">prompts/get</option>
          <option value="prompts/list">prompts/list</option>
          <option value="initialize">initialize</option>
        </select>
        <select
          value={successFilter}
          onChange={e => { setSuccessFilter(e.target.value as '' | 'true' | 'false'); setPage(0) }}
          style={{ minWidth: 90 }}
        >
          <option value="">全部状态</option>
          <option value="true">仅成功</option>
          <option value="false">仅失败</option>
        </select>
        <span className="filter-count" style={{ marginLeft: 'auto' }}>{total} 条</span>
      </div>

      {/* Table */}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>Combo</th>
              <th>方法</th>
              <th>工具 / 目标</th>
              <th>Provider</th>
              <th>User ID</th>
              <th>传输协议</th>
              <th>耗时</th>
              <th>状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={10} style={{ textAlign: 'center', color: 'var(--text-3)', padding: 36 }}>
                  暂无 MCP 调用记录。当下游机器人或客户端发起工具调用时，明细将实时记录于此。
                </td>
              </tr>
            )}
            {rows.map(r => {
              const mStyle = METHOD_COLOR[r.method] || { fg: 'var(--text-2)', bg: 'var(--bg)' }
              const isSuccess = r.success === 1
              return (
                <tr
                  key={r.id}
                  onClick={() => setSelectedRow(r)}
                  style={{ cursor: 'pointer' }}
                  title="点击查看入参和返回结果"
                >
                  <td style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }} title={formatFullDate(r.ts)}>
                    {formatTime(r.ts)}
                  </td>
                  <td>
                    <code style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--accent)', fontWeight: 600 }}>
                      {r.combo}
                    </code>
                  </td>
                  <td>
                    <span
                      className="tag"
                      style={{
                        fontSize: 10,
                        fontWeight: 600,
                        color: mStyle.fg,
                        background: mStyle.bg,
                      }}
                    >
                      {r.method}
                    </span>
                  </td>
                  <td>
                    {r.tool_name ? (
                      <code style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>{r.tool_name}</code>
                    ) : (
                      <span style={{ color: 'var(--text-3)' }}>-</span>
                    )}
                  </td>
                  <td>
                    {r.provider ? (
                      <span style={{ fontSize: 12, fontWeight: 500 }}>{r.provider}</span>
                    ) : (
                      <span style={{ color: 'var(--text-3)' }}>-</span>
                    )}
                  </td>
                  <td>
                    {r.user_id ? (
                      <span
                        className="tag"
                        style={{ fontSize: 11, background: 'var(--bg)', border: '1px solid var(--border-md)' }}
                      >
                        {r.user_id}
                      </span>
                    ) : (
                      <span style={{ color: 'var(--text-3)', fontSize: 11 }}>-</span>
                    )}
                  </td>
                  <td>
                    <span className="tag" style={{ fontSize: 10, textTransform: 'uppercase' }}>
                      {r.transport}
                    </span>
                  </td>
                  <td style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>
                    {r.duration_ms != null ? `${r.duration_ms}ms` : '-'}
                  </td>
                  <td>
                    {isSuccess ? (
                      <span className="tag" style={{ fontSize: 10, color: 'var(--ok-fg)', background: 'var(--ok-bg)' }}>
                        ✓ {r.status_code || 200}
                      </span>
                    ) : (
                      <span
                        className="tag"
                        style={{ fontSize: 10, color: 'var(--err-fg)', background: 'var(--err-bg)' }}
                        title={r.error || ''}
                      >
                        ✗ {r.status_code || 'Error'}
                      </span>
                    )}
                  </td>
                  <td>
                    <button
                      className="btn btn-sm"
                      style={{ fontSize: 11, padding: '2px 8px' }}
                      onClick={e => {
                        e.stopPropagation()
                        setSelectedRow(r)
                      }}
                    >
                      详情 ➜
                    </button>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="pagination" style={{ marginTop: 16, display: 'flex', justifyContent: 'flex-end', gap: 8, alignItems: 'center' }}>
          <button
            className="btn btn-sm"
            disabled={page === 0}
            onClick={() => setPage(p => Math.max(0, p - 1))}
          >
            ‹ 上一页
          </button>
          <span style={{ fontSize: 12, color: 'var(--text-3)' }}>
            第 {page + 1} / {totalPages} 页
          </span>
          <button
            className="btn btn-sm"
            disabled={page + 1 >= totalPages}
            onClick={() => setPage(p => p + 1)}
          >
            下一页 ›
          </button>
        </div>
      )}

      {/* Detail Modal / Drawer */}
      {selectedRow && (
        <div
          style={{
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            backgroundColor: 'rgba(0,0,0,0.5)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 1000,
            padding: 16,
          }}
          onClick={() => setSelectedRow(null)}
        >
          <div
            style={{
              background: 'var(--bg-panel)',
              border: '1px solid var(--border)',
              borderRadius: 10,
              width: '100%',
              maxWidth: 720,
              maxHeight: '85vh',
              display: 'flex',
              flexDirection: 'column',
              boxShadow: '0 20px 40px rgba(0,0,0,0.25)',
              overflow: 'hidden',
            }}
            onClick={e => e.stopPropagation()}
          >
            {/* Header */}
            <div
              style={{
                padding: '14px 18px',
                borderBottom: '1px solid var(--border)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                <span style={{ fontWeight: 600, fontSize: 15 }}>MCP 调用报文明细</span>
                <span
                  className="tag"
                  style={{
                    fontSize: 11,
                    color: selectedRow.success === 1 ? 'var(--ok-fg)' : 'var(--err-fg)',
                    background: selectedRow.success === 1 ? 'var(--ok-bg)' : 'var(--err-bg)',
                  }}
                >
                  {selectedRow.success === 1 ? '✓ 成功' : '✗ 失败'}
                </span>
              </div>
              <button className="btn btn-sm" onClick={() => setSelectedRow(null)}>
                ✕ 关闭
              </button>
            </div>

            {/* Body */}
            <div style={{ padding: '16px 20px', overflowY: 'auto', flex: 1 }}>
              {/* Meta details grid */}
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))',
                  gap: 10,
                  background: 'var(--bg)',
                  border: '1px solid var(--border)',
                  borderRadius: 6,
                  padding: '12px 14px',
                  marginBottom: 16,
                  fontSize: 12,
                }}
              >
                <div>
                  <span style={{ color: 'var(--text-3)' }}>时间: </span>
                  <div>{formatFullDate(selectedRow.ts)}</div>
                </div>
                <div>
                  <span style={{ color: 'var(--text-3)' }}>Combo: </span>
                  <div>
                    <code style={{ color: 'var(--accent)', fontWeight: 600 }}>{selectedRow.combo}</code>
                  </div>
                </div>
                <div>
                  <span style={{ color: 'var(--text-3)' }}>方法: </span>
                  <div>
                    <span className="tag" style={{ fontSize: 10 }}>{selectedRow.method}</span>
                  </div>
                </div>
                <div>
                  <span style={{ color: 'var(--text-3)' }}>工具/目标: </span>
                  <div>
                    <code style={{ fontWeight: 600 }}>{selectedRow.tool_name || '-'}</code>
                  </div>
                </div>
                <div>
                  <span style={{ color: 'var(--text-3)' }}>Provider: </span>
                  <div>{selectedRow.provider || '-'}</div>
                </div>
                <div>
                  <span style={{ color: 'var(--text-3)' }}>User ID: </span>
                  <div>{selectedRow.user_id || '-'}</div>
                </div>
                <div>
                  <span style={{ color: 'var(--text-3)' }}>传输协议: </span>
                  <div>{selectedRow.transport}</div>
                </div>
                <div>
                  <span style={{ color: 'var(--text-3)' }}>耗时: </span>
                  <div>{selectedRow.duration_ms != null ? `${selectedRow.duration_ms} ms` : '-'}</div>
                </div>
              </div>

              {/* Error box if failed */}
              {selectedRow.error && (
                <div className="alert err" style={{ marginBottom: 16 }}>
                  <strong>错误信息: </strong>
                  {selectedRow.error}
                </div>
              )}

              {/* Arguments (入参) */}
              <div style={{ marginBottom: 16 }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
                  <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-2)' }}>
                    请求参数 (Arguments / Params)
                  </span>
                  {selectedRow.arguments && (
                    <button
                      className="btn btn-sm"
                      style={{ fontSize: 11, padding: '2px 8px' }}
                      onClick={() => copyText('args', formatJSON(selectedRow.arguments))}
                    >
                      {copiedKey === 'args' ? '✓ 已复制' : '📋 复制代码'}
                    </button>
                  )}
                </div>
                <pre
                  style={{
                    margin: 0,
                    padding: '10px 14px',
                    background: 'var(--bg)',
                    border: '1px solid var(--border)',
                    borderRadius: 6,
                    fontFamily: 'var(--font-mono)',
                    fontSize: 12,
                    lineHeight: 1.45,
                    maxHeight: 200,
                    overflow: 'auto',
                  }}
                >
                  {formatJSON(selectedRow.arguments)}
                </pre>
              </div>

              {/* Result (出参) */}
              <div>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
                  <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-2)' }}>
                    返回结果 (Result / Response Data)
                  </span>
                  {selectedRow.result && (
                    <button
                      className="btn btn-sm"
                      style={{ fontSize: 11, padding: '2px 8px' }}
                      onClick={() => copyText('res', formatJSON(selectedRow.result))}
                    >
                      {copiedKey === 'res' ? '✓ 已复制' : '📋 复制代码'}
                    </button>
                  )}
                </div>
                <pre
                  style={{
                    margin: 0,
                    padding: '10px 14px',
                    background: 'var(--bg)',
                    border: '1px solid var(--border)',
                    borderRadius: 6,
                    fontFamily: 'var(--font-mono)',
                    fontSize: 12,
                    lineHeight: 1.45,
                    maxHeight: 260,
                    overflow: 'auto',
                  }}
                >
                  {formatJSON(selectedRow.result)}
                </pre>
              </div>
            </div>

            {/* Footer */}
            <div style={{ padding: '10px 18px', borderTop: '1px solid var(--border)', display: 'flex', justifyContent: 'flex-end' }}>
              <button className="btn" onClick={() => setSelectedRow(null)}>
                关闭
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
