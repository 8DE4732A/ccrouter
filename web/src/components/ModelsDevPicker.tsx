import { useCallback, useEffect, useRef, useState } from 'react'
import { refreshModelsDev, searchModelsDev } from '../api/client'
import type { ModelsDevEntry, ModelsDevStatus } from '../api/client'

type Props = {
  open: boolean
  initialQuery?: string
  providerHint?: string
  onClose: () => void
  onSelect: (entry: ModelsDevEntry) => void
}

function fmtTokens(v?: number): string {
  if (v == null) return '—'
  if (v >= 1_000_000) return (v / 1_000_000).toFixed(v % 1_000_000 === 0 ? 0 : 1) + 'M'
  if (v >= 1_000) return (v / 1_000).toFixed(v % 1_000 === 0 ? 0 : 1) + 'K'
  return String(v)
}

function fmtPrice(v?: number): string {
  if (v == null) return '—'
  if (v === 0) return '$0'
  return '$' + v.toLocaleString(undefined, { maximumFractionDigits: 4 })
}

// ModelsDevPicker is a searchable modal over the models.dev catalog. Selecting
// a row hands the entry back so the caller can snapshot its limits and prices
// into the combo member config.
export default function ModelsDevPicker({ open, initialQuery, providerHint, onClose, onSelect }: Props) {
  const [query, setQuery] = useState('')
  const [items, setItems] = useState<ModelsDevEntry[]>([])
  const [status, setStatus] = useState<ModelsDevStatus | null>(null)
  const [loading, setLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [err, setErr] = useState('')
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = useCallback(async () => {
    const res = await searchModelsDev({ q: query.trim(), provider: providerHint, limit: 50 })
    setItems(res.items)
    setStatus(res.status)
  }, [query, providerHint])

  useEffect(() => {
    if (open) {
      setQuery(initialQuery ?? '')
      setErr('')
    }
  }, [open, initialQuery])

  useEffect(() => {
    if (!open) return
    if (timer.current) clearTimeout(timer.current)
    timer.current = setTimeout(async () => {
      setLoading(true)
      setErr('')
      try {
        await load()
      } catch (e: unknown) {
        setErr(String(e))
      }
      setLoading(false)
    }, 250)
    return () => {
      if (timer.current) clearTimeout(timer.current)
    }
  }, [open, load])

  useEffect(() => {
    if (!open || !status || status.ready || status.disabled) return
    // The catalog is fetched in the background on first use. Poll briefly so
    // the picker fills in as soon as the snapshot is available.
    let attempts = 0
    const t = setInterval(async () => {
      attempts++
      if (attempts > 10) {
        clearInterval(t)
        return
      }
      try {
        await load()
      } catch {
        // keep polling; errors are already surfaced from the initial search
      }
    }, 2000)
    return () => clearInterval(t)
  }, [open, status?.ready, status?.disabled, load])

  if (!open) return null

  const refresh = async () => {
    setRefreshing(true)
    setErr('')
    try {
      await refreshModelsDev()
      await load()
    } catch (e: unknown) {
      setErr(String(e))
    }
    setRefreshing(false)
  }

  return (
    <div
      style={{
        position: 'fixed', inset: 0, background: 'rgba(0,0,0,0.5)',
        display: 'flex', alignItems: 'center', justifyContent: 'center',
        zIndex: 1000, padding: 16,
      }}
      onClick={onClose}
    >
      <div
        style={{
          background: 'var(--bg-panel)', border: '1px solid var(--border)', borderRadius: 10,
          width: '100%', maxWidth: 900, maxHeight: '85vh', display: 'flex', flexDirection: 'column',
          boxShadow: '0 20px 40px rgba(0,0,0,0.2)', overflow: 'hidden',
        }}
        onClick={e => e.stopPropagation()}
      >
        <div style={{
          padding: '14px 18px', borderBottom: '1px solid var(--border)',
          display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12,
        }}>
          <div style={{ fontWeight: 600, fontSize: 15 }}>从 models.dev 选择模型</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <button className="btn btn-sm" onClick={refresh} disabled={refreshing || status?.disabled}>
              {refreshing ? '刷新中…' : '刷新目录'}
            </button>
            <button className="btn btn-sm" onClick={onClose}>✕ 关闭</button>
          </div>
        </div>

        <div style={{ padding: '12px 18px', borderBottom: '1px solid var(--border)' }}>
          <input
            autoFocus
            value={query}
            placeholder="搜索 provider 或模型 ID，例如 deepseek、claude-opus"
            onChange={e => setQuery(e.target.value)}
            style={{ width: '100%' }}
          />
          <div style={{ marginTop: 8, fontSize: 11, color: 'var(--text-3)', display: 'flex', gap: 12, flexWrap: 'wrap' }}>
            {status?.disabled && <span>models.dev 已禁用，请在设置中启用或手动填写参数。</span>}
            {!status?.disabled && status?.ready && (
              <span>
                已加载 {status.provider_count ?? 0} 个 provider / {status.model_count ?? 0} 个模型
                {status.stale ? '（数据已过期，可点击刷新目录）' : ''}
              </span>
            )}
            {!status?.disabled && !status?.ready && <span>正在加载 models.dev 目录…</span>}
            {status?.last_error && <span style={{ color: 'var(--warn-fg)' }}>错误: {status.last_error}</span>}
            {providerHint && <span>Provider 提示: {providerHint}</span>}
            <span title="models.dev 的阶梯定价（cost.tiers）不会被展开，填充的是基础档位价格">
              价格为 models.dev 基础档位
            </span>
          </div>
        </div>

        <div style={{ overflowY: 'auto', flex: 1 }}>
          {err && <div className="alert err" style={{ margin: 12 }}>{err}</div>}
          {loading && items.length === 0 && <div className="empty-state" style={{ padding: 40 }}>搜索中…</div>}
          {!loading && items.length === 0 && !err && (
            <div className="empty-state" style={{ padding: 40 }}>
              {status && !status.ready ? '目录尚未就绪，请稍候或点击「刷新目录」' : '没有匹配的模型'}
            </div>
          )}
          {items.length > 0 && (
            <table>
              <thead>
                <tr>
                  <th>Provider</th>
                  <th>Model</th>
                  <th>Context</th>
                  <th>Max Out</th>
                  <th>输入 $/1M</th>
                  <th>输出 $/1M</th>
                  <th>缓存读 $/1M</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {items.map(e => (
                  <tr key={e.source_id}>
                    <td style={{ fontSize: 12 }}>{e.provider_name || e.provider_id}</td>
                    <td>
                      <div className="mono" style={{ fontSize: 12 }}>{e.model_id}</div>
                      {e.name && e.name !== e.model_id && (
                        <div style={{ fontSize: 11, color: 'var(--text-3)' }}>{e.name}</div>
                      )}
                    </td>
                    <td>{fmtTokens(e.context_window)}</td>
                    <td>{fmtTokens(e.max_output_tokens)}</td>
                    <td>{fmtPrice(e.cost?.input)}</td>
                    <td>{fmtPrice(e.cost?.output)}</td>
                    <td>{fmtPrice(e.cost?.cache_read)}</td>
                    <td>
                      <button className="btn btn-sm" onClick={() => { onSelect(e); onClose() }}>
                        使用
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  )
}
