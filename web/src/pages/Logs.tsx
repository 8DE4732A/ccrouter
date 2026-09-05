import { useEffect, useState } from 'react'
import { getLogs, getLogDetail, getLogSettings, putLogSettings } from '../api/client'
import type { LogRow, LogRecord } from '../api/client'
import { FMT_COLOR } from '../api/client'

const PAGE_SIZE = 20

export default function Logs() {
  const [rows, setRows] = useState<LogRow[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [page, setPage] = useState(0)
  const [successFilter, setSuccessFilter] = useState<'' | 'true' | 'false'>('')
  const [err, setErr] = useState('')

  const [verbose, setVerbose] = useState(false)
  const [dir, setDir] = useState('logs')
  const [maxFileSizeMB, setMaxFileSizeMB] = useState(20)
  const [maxBackups, setMaxBackups] = useState(10)
  const [compressionLevel, setCompressionLevel] = useState('best')
  const [showConfig, setShowConfig] = useState(false)
  const [savingConfig, setSavingConfig] = useState(false)
  const [settingsMsg, setSettingsMsg] = useState('')
  const [settingsErr, setSettingsErr] = useState('')

  // expandedTs → null (loading) | LogRecord (loaded) | string (error)
  const [expanded, setExpanded] = useState<Map<number, LogRecord | null | string>>(new Map())

  useEffect(() => {
    getLogSettings()
      .then(s => {
        setVerbose(s.verbose_logging)
        if (s.dir) setDir(s.dir)
        if (s.max_file_size_mb) setMaxFileSizeMB(s.max_file_size_mb)
        if (s.max_backups) setMaxBackups(s.max_backups)
        if (s.compression_level) setCompressionLevel(s.compression_level)
      })
      .catch(e => setSettingsErr(String(e)))
  }, [])

  const load = async () => {
    setErr('')
    try {
      const res = await getLogs({
        limit: PAGE_SIZE,
        offset: page * PAGE_SIZE,
        success: successFilter === '' ? undefined : successFilter === 'true',
      })
      setRows(res.items)
      setHasMore(res.has_more)
      setExpanded(new Map()) // reset expanded state on new page
    } catch (e: unknown) { setErr(String(e)) }
  }

  useEffect(() => { load() }, [page, successFilter])

  const toggleVerbose = async (next: boolean) => {
    setSettingsErr('')
    setSettingsMsg('')
    try {
      const res = await putLogSettings({
        enabled: next,
        dir,
        max_file_size_mb: maxFileSizeMB,
        max_backups: maxBackups,
        compression_level: compressionLevel,
      })
      setVerbose(res.verbose_logging)
    } catch (e: unknown) {
      setSettingsErr(String(e))
    }
  }

  const saveAdvancedConfig = async () => {
    setSavingConfig(true)
    setSettingsErr('')
    setSettingsMsg('')
    try {
      const res = await putLogSettings({
        enabled: verbose,
        dir: dir.trim() || 'logs',
        max_file_size_mb: Number(maxFileSizeMB) || 20,
        max_backups: Number(maxBackups) || 10,
        compression_level: compressionLevel,
      })
      setVerbose(res.verbose_logging)
      if (res.dir) setDir(res.dir)
      if (res.max_file_size_mb) setMaxFileSizeMB(res.max_file_size_mb)
      if (res.max_backups) setMaxBackups(res.max_backups)
      if (res.compression_level) setCompressionLevel(res.compression_level)
      setSettingsMsg('✓ 高级日志配置已保存并实时生效')
      setTimeout(() => setSettingsMsg(''), 4000)
    } catch (e: unknown) {
      setSettingsErr(String(e))
    } finally {
      setSavingConfig(false)
    }
  }

  const toggleRow = async (ts: number) => {
    setExpanded(prev => {
      const next = new Map(prev)
      if (next.has(ts)) {
        next.delete(ts) // collapse
        return next
      }
      next.set(ts, null) // mark as loading
      return next
    })

    // If not already loaded, fetch detail
    if (!expanded.has(ts)) {
      try {
        const detail = await getLogDetail(ts)
        setExpanded(prev => {
          const next = new Map(prev)
          next.set(ts, detail)
          return next
        })
      } catch (e: unknown) {
        setExpanded(prev => {
          const next = new Map(prev)
          next.set(ts, String(e))
          return next
        })
      }
    }
  }

  return (
    <div className="page">
      <div className="page-header">
        <span className="page-title">日志</span>
        <span className="page-sub">完整请求报文记录（点击行展开明细）</span>
      </div>

      {/* Verbose toggle & Advanced settings */}
      <div style={{ marginBottom: 16 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <label style={{
            display: 'flex', alignItems: 'center', gap: 8,
            padding: '6px 12px',
            border: `1px solid ${verbose ? 'var(--accent)' : 'var(--border-md)'}`,
            borderRadius: 6,
            background: verbose ? 'var(--accent-light)' : 'var(--bg-input)',
            cursor: 'pointer', userSelect: 'none',
          }}>
            <input type="checkbox" checked={verbose} onChange={e => toggleVerbose(e.target.checked)} />
            <span style={{ fontWeight: 600, fontSize: 13 }}>详细记录</span>
          </label>

          <button
            type="button"
            onClick={() => setShowConfig(prev => !prev)}
            style={{
              display: 'flex', alignItems: 'center', gap: 6,
              padding: '6px 12px', fontSize: 13,
              background: showConfig ? 'var(--bg-hover)' : 'var(--bg-input)',
              border: '1px solid var(--border)',
              borderRadius: 6, cursor: 'pointer',
            }}
          >
            <span>⚙️ 高级配置</span>
            <span style={{ fontSize: 11, color: 'var(--text-3)' }}>{showConfig ? '▲ 收起' : '▼ 展开'}</span>
          </button>

          <span style={{ fontSize: 12, color: 'var(--warn-fg)', display: 'flex', alignItems: 'center', gap: 4 }}>
            ⚠ 完整记录报文含明文 API 密钥，仅限本地使用，切勿暴露公网
          </span>
          {settingsErr && !showConfig && <span style={{ fontSize: 12, color: 'var(--err-fg)' }}>{settingsErr}</span>}
          {settingsMsg && !showConfig && <span style={{ fontSize: 12, color: 'var(--ok-fg)' }}>{settingsMsg}</span>}
        </div>

        {/* Advanced settings drawer */}
        {showConfig && (
          <div style={{
            marginTop: 12,
            padding: 16,
            background: 'var(--bg-panel)',
            border: '1px solid var(--border)',
            borderRadius: 8,
            maxWidth: 760,
          }}>
            <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 12, color: 'var(--text-1)' }}>
              详细请求日志存储配置
            </div>

            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 14 }}>
              <div>
                <label style={{ display: 'block', fontSize: 12, color: 'var(--text-2)', marginBottom: 4 }}>
                  存储路径 (Log Directory)
                </label>
                <input
                  type="text"
                  value={dir}
                  onChange={e => setDir(e.target.value)}
                  placeholder="logs"
                  style={{ width: '100%', fontFamily: 'var(--font-mono)', fontSize: 12 }}
                />
                <span style={{ fontSize: 11, color: 'var(--text-3)' }}>相对或绝对路径，默认 logs</span>
              </div>

              <div>
                <label style={{ display: 'block', fontSize: 12, color: 'var(--text-2)', marginBottom: 4 }}>
                  单个文件大小 (MB)
                </label>
                <input
                  type="number"
                  min={1}
                  max={2048}
                  value={maxFileSizeMB}
                  onChange={e => setMaxFileSizeMB(Number(e.target.value))}
                  style={{ width: '100%', fontFamily: 'var(--font-mono)', fontSize: 12 }}
                />
                <span style={{ fontSize: 11, color: 'var(--text-3)' }}>超过后自动切片轮转 (默认 20MB)</span>
              </div>

              <div>
                <label style={{ display: 'block', fontSize: 12, color: 'var(--text-2)', marginBottom: 4 }}>
                  保留文件数量 (Max Backups)
                </label>
                <input
                  type="number"
                  min={1}
                  max={100}
                  value={maxBackups}
                  onChange={e => setMaxBackups(Number(e.target.value))}
                  style={{ width: '100%', fontFamily: 'var(--font-mono)', fontSize: 12 }}
                />
                <span style={{ fontSize: 11, color: 'var(--text-3)' }}>超出上限的最旧切片自动删除 (默认 10)</span>
              </div>

              <div>
                <label style={{ display: 'block', fontSize: 12, color: 'var(--text-2)', marginBottom: 4 }}>
                  压缩等级 (zstd Level)
                </label>
                <select
                  value={compressionLevel}
                  onChange={e => setCompressionLevel(e.target.value)}
                  style={{ width: '100%', fontSize: 12 }}
                >
                  <option value="fastest">fastest（极速，CPU占用最低）</option>
                  <option value="default">default（标准平衡）</option>
                  <option value="better">better（高压缩比）</option>
                  <option value="best">best（极致压缩，推荐，默认）</option>
                </select>
                <span style={{ fontSize: 11, color: 'var(--text-3)' }}>基于前序请求字典链压缩，节省磁盘</span>
              </div>
            </div>

            <div style={{ marginTop: 14, display: 'flex', alignItems: 'center', gap: 12 }}>
              <button
                type="button"
                className="btn-primary"
                onClick={saveAdvancedConfig}
                disabled={savingConfig}
                style={{ fontSize: 12, padding: '6px 16px' }}
              >
                {savingConfig ? '正在保存...' : '保存配置'}
              </button>
              {settingsErr && <span style={{ fontSize: 12, color: 'var(--err-fg)' }}>{settingsErr}</span>}
              {settingsMsg && <span style={{ fontSize: 12, color: 'var(--ok-fg)' }}>{settingsMsg}</span>}
            </div>
          </div>
        )}
      </div>

      {err && <div className="alert err">{err}</div>}

      <div className="filters">
        <select
          value={successFilter}
          onChange={e => { setSuccessFilter(e.target.value as '' | 'true' | 'false'); setPage(0) }}
          style={{ minWidth: 90 }}
        >
          <option value="">全部状态</option>
          <option value="true">仅成功</option>
          <option value="false">仅失败</option>
        </select>
        <button onClick={() => { setPage(0); load() }} style={{ fontSize: 12 }}>刷新</button>
      </div>

      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>时间</th>
              <th>Combo</th>
              <th>Provider › Model</th>
              <th>格式</th>
              <th>流式</th>
              <th>状态</th>
              <th>耗时</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={8} style={{ textAlign: 'center', color: 'var(--text-3)', padding: 32 }}>
                  {verbose ? '暂无数据' : '请先开启详细记录'}
                </td>
              </tr>
            )}
            {rows.map((r, i) => {
              const detail = expanded.get(r.ts)
              const isOpen = expanded.has(r.ts)
              const isLoading = detail === null
              return (
                <>
                  <tr
                    key={i}
                    className={r.success ? '' : 'row-err'}
                    style={{ cursor: 'pointer' }}
                    onClick={() => toggleRow(r.ts)}
                  >
                    <td className="text-muted" style={{ fontSize: 12, whiteSpace: 'nowrap' }}>
                      {new Date(r.ts * 1000).toLocaleString('zh-CN')}
                    </td>
                    <td><code>{r.combo ?? '—'}</code></td>
                    <td style={{ fontSize: 12 }}>{r.provider ?? '—'} › {r.model ?? '—'}</td>
                    <td>
                      {r.api_format && (
                        <span className={`tag ${(FMT_COLOR as Record<string, string>)[r.api_format] ?? 'amber'}`}>
                          {r.api_format}
                        </span>
                      )}
                    </td>
                    <td style={{ color: r.is_stream ? 'var(--ok-fg)' : 'var(--text-3)' }}>
                      {r.is_stream ? '✓' : '—'}
                    </td>
                    <td>
                      {r.success
                        ? <span className="badge ok">{r.status_code}</span>
                        : <span className="badge err">{r.status_code ?? 'ERR'}</span>}
                    </td>
                    <td className="text-muted">{r.duration_ms != null ? r.duration_ms + ' ms' : '—'}</td>
                    <td style={{ color: 'var(--text-3)', fontSize: 12 }}>
                      {isLoading ? '…' : isOpen ? '▲' : '▼'}
                    </td>
                  </tr>
                  {isOpen && (
                    <tr key={`${i}-detail`}>
                      <td colSpan={8} style={{ padding: 0 }}>
                        {isLoading
                          ? <div style={{ padding: '12px 16px', color: 'var(--text-3)', fontSize: 12 }}>加载中…</div>
                          : typeof detail === 'string'
                            ? <div style={{ padding: '12px 16px', color: 'var(--err-fg)', fontSize: 12 }}>{detail}</div>
                            : <LogDetail record={detail as LogRecord} />
                        }
                      </td>
                    </tr>
                  )}
                </>
              )
            })}
          </tbody>
        </table>
      </div>

      <div className="pagination">
        <button disabled={page === 0} onClick={() => setPage(p => p - 1)}>← 上一页</button>
        <span>第 {page + 1} 页</span>
        <button disabled={!hasMore} onClick={() => setPage(p => p + 1)}>下一页 →</button>
      </div>
    </div>
  )
}

function LogDetail({ record }: { record: LogRecord }) {
  const sections: [string, unknown][] = [
    ['客户端请求 (Client Request)', record.request?.client],
    ['上游请求 (Upstream Request)', record.request?.upstream],
    ['响应 (Response)', record.response],
  ]

  // Extract preview images if this is an images response (generations or edits)
  const respBody = (record.response as Record<string, unknown> | undefined)?.body as Record<string, unknown> | undefined
  const respImages: string[] = []
  if (respBody && Array.isArray(respBody.data)) {
    for (const item of respBody.data as Array<Record<string, unknown>>) {
      if (item && typeof item.url === 'string' && item.url.startsWith('http')) {
        respImages.push(item.url)
      } else if (item && typeof item.b64_json === 'string' && item.b64_json.length > 50 && !item.b64_json.includes('truncated')) {
        respImages.push(`data:image/png;base64,${item.b64_json}`)
      }
    }
  }

  // Check if client request was multipart/form-data
  const clientBody = (record.request?.client as Record<string, unknown> | undefined)?.body as Record<string, unknown> | undefined
  const isMultipart = clientBody && clientBody._type === 'multipart/form-data'
  const multipartFiles = isMultipart && Array.isArray(clientBody.files) ? (clientBody.files as Array<Record<string, unknown>>) : []

  return (
    <div style={{
      background: 'var(--bg-panel)',
      borderTop: '1px solid var(--border)',
      borderBottom: '1px solid var(--border)',
      padding: '12px 16px',
    }}>
      {respImages.length > 0 && (
        <div style={{ marginBottom: 14 }}>
          <div style={{
            fontSize: 11, fontWeight: 600, textTransform: 'uppercase',
            letterSpacing: '0.06em', color: 'var(--text-3)', marginBottom: 6,
          }}>
            生成/编辑图像预览 ({respImages.length})
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 10 }}>
            {respImages.map((src, idx) => (
              <a key={idx} href={src} target="_blank" rel="noopener noreferrer">
                <img
                  src={src}
                  alt={`预览 ${idx + 1}`}
                  style={{
                    maxHeight: 180, maxWidth: 240, objectFit: 'contain',
                    borderRadius: 4, border: '1px solid var(--border)',
                    background: 'var(--bg)',
                  }}
                />
              </a>
            ))}
          </div>
        </div>
      )}

      {isMultipart && multipartFiles.length > 0 && (
        <div style={{ marginBottom: 12, display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <span style={{ fontSize: 11, color: 'var(--text-3)', fontWeight: 600 }}>附件文件:</span>
          {multipartFiles.map((f, idx) => (
            <span key={idx} className="tag blue" style={{ fontSize: 11 }}>
              📎 {String(f.field)}: {String(f.filename || 'blob')} ({f.size_bytes != null ? `${((Number(f.size_bytes)) / 1024).toFixed(1)} KB` : ''})
            </span>
          ))}
        </div>
      )}

      {sections.map(([title, data]) => (
        <div key={title} style={{ marginBottom: 12 }}>
          <div style={{
            fontSize: 11, fontWeight: 600, textTransform: 'uppercase',
            letterSpacing: '0.06em', color: 'var(--text-3)', marginBottom: 4,
          }}>
            {title}
          </div>
          <pre style={{
            fontFamily: 'var(--font-mono)', fontSize: 12,
            whiteSpace: 'pre-wrap', wordBreak: 'break-word',
            overflowY: 'auto', maxHeight: 360,
            background: 'var(--bg)', border: '1px solid var(--border)',
            borderRadius: 4, padding: '8px 10px', margin: 0,
          }}>
            {JSON.stringify(data ?? null, null, 2)}
          </pre>
        </div>
      ))}
    </div>
  )
}
