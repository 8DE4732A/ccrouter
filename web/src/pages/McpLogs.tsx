import { useEffect, useRef, useState } from 'react'
import { getMcpRequests } from '../api/client'
import type { McpRequestRow } from '../api/client'

const PAGE_SIZE = 25

const METHOD_COLOR: Record<string, { fg: string; bg: string }> = {
  'tools/call': { fg: 'var(--accent)', bg: 'var(--accent-light)' },
  'tools/list': { fg: '#0284c7', bg: 'rgba(2, 132, 199, 0.1)' },
  'resources/read': { fg: '#10b981', bg: 'rgba(16, 185, 129, 0.1)' },
  'resources/list': { fg: '#059669', bg: 'rgba(5, 150, 105, 0.1)' },
  'prompts/get': { fg: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.1)' },
  'prompts/list': { fg: '#7c3aed', bg: 'rgba(124, 58, 237, 0.1)' },
  'initialize': { fg: '#f59e0b', bg: 'rgba(245, 158, 11, 0.1)' },
}

function prettyJSON(raw: string | null): string {
  if (!raw) return '—'
  try {
    const parsed = JSON.parse(raw)
    return JSON.stringify(parsed, null, 2)
  } catch {
    return raw
  }
}

function CopyBtn({ text, label }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }
  return (
    <button
      type="button"
      onClick={handleCopy}
      style={{
        fontSize: 11,
        padding: '2px 8px',
        borderRadius: 4,
        border: '1px solid var(--border)',
        background: copied ? 'var(--accent-light)' : 'var(--bg-input)',
        color: copied ? 'var(--accent)' : 'var(--text-2)',
        cursor: 'pointer',
      }}
    >
      {copied ? '✓ 已复制' : label ?? '复制'}
    </button>
  )
}

export default function McpLogs() {
  const [rows, setRows] = useState<McpRequestRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0)
  const [search, setSearch] = useState('')
  const [method, setMethod] = useState('')
  const [transport, setTransport] = useState('')
  const [successFilter, setSuccessFilter] = useState<'' | 'true' | 'false'>('')
  const [autoRefresh, setAutoRefresh] = useState(false)
  const [err, setErr] = useState('')

  // Expanded row IDs
  const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set())

  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const load = async () => {
    try {
      const res = await getMcpRequests({
        limit: PAGE_SIZE,
        offset: page * PAGE_SIZE,
        q: search.trim() || undefined,
        method: method || undefined,
        transport: transport || undefined,
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
  }, [page, search, method, transport, successFilter])

  useEffect(() => {
    if (autoRefresh) {
      timerRef.current = setInterval(load, 5000)
    } else if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current)
        timerRef.current = null
      }
    }
  }, [autoRefresh, page, search, method, transport, successFilter])

  const toggleRow = (id: number) => {
    setExpandedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) {
        next.delete(id)
      } else {
        next.add(id)
      }
      return next
    })
  }

  const expandAll = () => {
    setExpandedIds(new Set(rows.map(r => r.id)))
  }

  const collapseAll = () => {
    setExpandedIds(new Set())
  }

  const totalPages = Math.ceil(total / PAGE_SIZE)

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <span className="page-title">日志</span>
          <span className="page-sub" style={{ marginLeft: 12 }}>完整报文与实时执行日志（点击行展开明细）</span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
          <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 13, cursor: 'pointer' }}>
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={e => setAutoRefresh(e.target.checked)}
            />
            <span>5s 自动刷新</span>
          </label>
          <button className="refresh-btn" onClick={() => load()}>
            刷新
          </button>
        </div>
      </div>

      {err && <div className="alert err">{err}</div>}

      {/* Filter toolbar */}
      <div className="filters" style={{ flexWrap: 'wrap', gap: 8, marginBottom: 16 }}>
        <input
          type="text"
          value={search}
          onChange={e => { setSearch(e.target.value); setPage(0) }}
          placeholder="搜索关键词 (工具名、上游、错误、入参)..."
          style={{ minWidth: 260, flex: '1 1 240px' }}
        />

        <select
          value={method}
          onChange={e => { setMethod(e.target.value); setPage(0) }}
          style={{ minWidth: 120 }}
        >
          <option value="">全部方法 (All Methods)</option>
          <option value="tools/call">tools/call (工具执行)</option>
          <option value="tools/list">tools/list (工具清单)</option>
          <option value="resources/read">resources/read (资源读取)</option>
          <option value="resources/list">resources/list (资源清单)</option>
          <option value="prompts/get">prompts/get (提示词)</option>
          <option value="prompts/list">prompts/list (提示词清单)</option>
          <option value="initialize">initialize (握手初始)</option>
        </select>

        <select
          value={transport}
          onChange={e => { setTransport(e.target.value); setPage(0) }}
          style={{ minWidth: 110 }}
        >
          <option value="">全部通道</option>
          <option value="http">HTTP POST</option>
          <option value="sse">SSE 长连接</option>
          <option value="playground">Playground</option>
        </select>

        <select
          value={successFilter}
          onChange={e => { setSuccessFilter(e.target.value as '' | 'true' | 'false'); setPage(0) }}
          style={{ minWidth: 100 }}
        >
          <option value="">全部状态</option>
          <option value="true">仅成功</option>
          <option value="false">仅失败</option>
        </select>

        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 6 }}>
          <button
            type="button"
            onClick={expandAll}
            style={{ fontSize: 12, padding: '4px 8px', background: 'transparent', border: '1px solid var(--border)' }}
          >
            全部展开
          </button>
          <button
            type="button"
            onClick={collapseAll}
            style={{ fontSize: 12, padding: '4px 8px', background: 'transparent', border: '1px solid var(--border)' }}
          >
            全部折叠
          </button>
        </div>
      </div>

      {/* Logs Table */}
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th style={{ width: 150 }}>时间</th>
              <th>Combo</th>
              <th>Provider › Tool</th>
              <th>方法</th>
              <th>传输通道</th>
              <th>状态</th>
              <th>耗时</th>
              <th style={{ width: 40 }}></th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={8} style={{ textAlign: 'center', color: 'var(--text-3)', padding: 36 }}>
                  暂无匹配的 MCP 日志记录
                </td>
              </tr>
            )}
            {rows.map(r => {
              const isOpen = expandedIds.has(r.id)
              const mColor = METHOD_COLOR[r.method] ?? { fg: 'var(--text-2)', bg: 'var(--bg-input)' }
              const isOk = r.success === 1
              const argStr = prettyJSON(r.arguments)
              const resStr = prettyJSON(r.result)

              return (
                <>
                  <tr
                    key={r.id}
                    className={isOk ? '' : 'row-err'}
                    style={{ cursor: 'pointer', background: isOpen ? 'var(--bg-hover)' : undefined }}
                    onClick={() => toggleRow(r.id)}
                  >
                    <td className="text-muted" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
                      {new Date(r.ts * 1000).toLocaleString('zh-CN')}
                    </td>
                    <td><code>{r.combo}</code></td>
                    <td style={{ fontSize: 12 }}>
                      {r.provider ? <strong>{r.provider}</strong> : <span className="text-muted">—</span>}
                      {r.tool_name ? (
                        <>
                          <span style={{ color: 'var(--text-3)', margin: '0 4px' }}>›</span>
                          <span style={{ color: 'var(--accent)' }}>{r.tool_name}</span>
                        </>
                      ) : null}
                    </td>
                    <td>
                      <span
                        style={{
                          fontSize: 11,
                          fontWeight: 600,
                          padding: '2px 7px',
                          borderRadius: 4,
                          color: mColor.fg,
                          background: mColor.bg,
                          fontFamily: 'var(--font-mono)',
                        }}
                      >
                        {r.method}
                      </span>
                    </td>
                    <td>
                      <span className="badge" style={{ fontSize: 11 }}>{r.transport}</span>
                    </td>
                    <td>
                      {isOk ? (
                        <span className="badge ok">200 OK</span>
                      ) : (
                        <span className="badge err" title={r.error || ''}>
                          {r.status_code ? `${r.status_code} FAIL` : 'FAIL'}
                        </span>
                      )}
                    </td>
                    <td style={{ fontSize: 12 }}>
                      {r.duration_ms != null ? `${r.duration_ms} ms` : '—'}
                    </td>
                    <td style={{ textAlign: 'center', color: 'var(--text-3)', fontSize: 11 }}>
                      {isOpen ? '▲' : '▼'}
                    </td>
                  </tr>

                  {/* Expanded Detail Accordion Row */}
                  {isOpen && (
                    <tr key={`${r.id}-detail`} style={{ background: 'var(--bg-panel)' }}>
                      <td colSpan={8} style={{ padding: 0 }}>
                        <div style={{ padding: '16px 20px', borderBottom: '1px solid var(--border)' }}>
                          {/* Metadata Grid */}
                          <div
                            style={{
                              display: 'flex',
                              alignItems: 'center',
                              gap: 20,
                              flexWrap: 'wrap',
                              marginBottom: 14,
                              fontSize: 12,
                              color: 'var(--text-2)',
                              paddingBottom: 10,
                              borderBottom: '1px dashed var(--border-light)',
                            }}
                          >
                            <div><strong>User ID:</strong> <code>{r.user_id || '(none)'}</code></div>
                            <div><strong>Transport:</strong> <code>{r.transport}</code></div>
                            <div><strong>Method:</strong> <code>{r.method}</code></div>
                            <div><strong>Duration:</strong> {r.duration_ms != null ? `${r.duration_ms} ms` : '—'}</div>
                            <div><strong>Timestamp:</strong> {new Date(r.ts * 1000).toISOString()}</div>
                          </div>

                          {/* Error Banner */}
                          {r.error && (
                            <div
                              style={{
                                padding: '8px 12px',
                                background: 'rgba(239, 68, 68, 0.1)',
                                border: '1px solid rgba(239, 68, 68, 0.3)',
                                borderRadius: 6,
                                color: 'var(--err-fg)',
                                fontSize: 13,
                                marginBottom: 14,
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'space-between',
                              }}
                            >
                              <span><strong>错误信息:</strong> {r.error}</span>
                              <CopyBtn text={r.error} label="复制错误" />
                            </div>
                          )}

                          {/* Request & Response Side-by-Side Panels */}
                          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(360px, 1fr))', gap: 14 }}>
                            {/* Arguments */}
                            <div
                              style={{
                                background: 'var(--bg-card)',
                                border: '1px solid var(--border)',
                                borderRadius: 6,
                                overflow: 'hidden',
                              }}
                            >
                              <div
                                style={{
                                  padding: '8px 12px',
                                  background: 'var(--bg-hover)',
                                  borderBottom: '1px solid var(--border)',
                                  display: 'flex',
                                  alignItems: 'center',
                                  justifyContent: 'space-between',
                                  fontSize: 12,
                                  fontWeight: 600,
                                }}
                              >
                                <span>入参报文 (Arguments / Params)</span>
                                {r.arguments && <CopyBtn text={r.arguments} label="复制 JSON" />}
                              </div>
                              <pre
                                style={{
                                  margin: 0,
                                  padding: 12,
                                  fontSize: 12,
                                  fontFamily: 'var(--font-mono)',
                                  maxHeight: 280,
                                  overflowY: 'auto',
                                  whiteSpace: 'pre-wrap',
                                  wordBreak: 'break-all',
                                  lineHeight: 1.4,
                                }}
                              >
                                {argStr}
                              </pre>
                            </div>

                            {/* Result */}
                            <div
                              style={{
                                background: 'var(--bg-card)',
                                border: '1px solid var(--border)',
                                borderRadius: 6,
                                overflow: 'hidden',
                              }}
                            >
                              <div
                                style={{
                                  padding: '8px 12px',
                                  background: 'var(--bg-hover)',
                                  borderBottom: '1px solid var(--border)',
                                  display: 'flex',
                                  alignItems: 'center',
                                  justifyContent: 'space-between',
                                  fontSize: 12,
                                  fontWeight: 600,
                                }}
                              >
                                <span>返回报文 (Result / Payload)</span>
                                {r.result && <CopyBtn text={r.result} label="复制 JSON" />}
                              </div>
                              <pre
                                style={{
                                  margin: 0,
                                  padding: 12,
                                  fontSize: 12,
                                  fontFamily: 'var(--font-mono)',
                                  maxHeight: 280,
                                  overflowY: 'auto',
                                  whiteSpace: 'pre-wrap',
                                  wordBreak: 'break-all',
                                  lineHeight: 1.4,
                                }}
                              >
                                {resStr}
                              </pre>
                            </div>
                          </div>
                        </div>
                      </td>
                    </tr>
                  )}
                </>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      <div className="pagination" style={{ marginTop: 14, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <span style={{ fontSize: 13, color: 'var(--text-3)' }}>
          共 {total} 条记录，第 {page + 1} / {totalPages || 1} 页
        </span>
        <div style={{ display: 'flex', gap: 6 }}>
          <button
            onClick={() => setPage(p => Math.max(0, p - 1))}
            disabled={page === 0}
            style={{ fontSize: 12, padding: '4px 10px' }}
          >
            上一页
          </button>
          <button
            onClick={() => setPage(p => p + 1)}
            disabled={(page + 1) * PAGE_SIZE >= total}
            style={{ fontSize: 12, padding: '4px 10px' }}
          >
            下一页
          </button>
        </div>
      </div>
    </div>
  )
}
