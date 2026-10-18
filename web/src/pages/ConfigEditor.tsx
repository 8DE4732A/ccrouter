import { useEffect, useMemo, useState } from 'react'
import { getConfig, putConfig, FMT_ENDPOINT, FMT_COLOR, normalizeFormats, suggestModelsDev } from '../api/client'
import type {
  AppConfig, ProviderConfig, ComboConfig, HealthCheckRule, ComboMember, ApiEndpoint, ApiFormat,
  PayloadScript, GeneralConfig, ProxyConfig, LoggingConfig, ModelCost, ModelsDevEntry, ModelsDevConfig,
} from '../api/client'
import ModelsDevPicker from '../components/ModelsDevPicker'

// Client-facing formats (proxy routes exist for these).
const CLIENT_FORMATS: ApiFormat[] = ['openai', 'anthropic', 'openai-responses', 'openai-images', 'openai-image-edits', 'openai-embeddings', 'gemini']
// All formats including upstream-only (for upstream_api_format hint).
const ALL_FORMATS: ApiFormat[] = ['openai', 'anthropic', 'openai-responses', 'openai-images', 'openai-image-edits', 'openai-embeddings', 'gemini']

const EMPTY_ENDPOINT = (): ApiEndpoint => ({ api_format: 'openai', base_url: '' })
const EMPTY_RULE = (): HealthCheckRule => ({
  description: '', jsonpath: '$.error.type', match_value: '',
  match_type: 'equals', action: 'rotate', cooldown_seconds: 60, models: [], http_status_codes: [],
})
const EMPTY_PROVIDER = (): ProviderConfig => ({
  name: '', api: [EMPTY_ENDPOINT()], max_retries: 3,
  key_strategy: 'fill-first', keys: [{ key: '' }], health_check_rules: [],
})
const EMPTY_GENERAL = (): GeneralConfig => ({ api_keys: [], proxy: undefined, request_timeout_seconds: undefined })
const EMPTY_COMBO = (): ComboConfig => ({
  name: '', owned_by: 'default', is_default: true, api_format: ['openai'], strategy: 'fill-first',
  members: [{ provider: '', model: '' }], aliases: [],
})
const EMPTY_PAYLOAD_SCRIPT = (): PayloadScript => ({ name: '', enabled: true, script: '' })

// parseOptionalInt converts an <input type="number"> value, mapping empty or
// invalid input to undefined so the field is omitted from the saved config.
function parseOptionalInt(v: string): number | undefined {
  const n = parseInt(v, 10)
  return isNaN(n) ? undefined : n
}


// ── Icons ────────────────────────────────────────────────────────
function IconPlus() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
      <line x1="8" y1="2" x2="8" y2="14"/><line x1="2" y1="8" x2="14" y2="8"/>
    </svg>
  )
}
function IconTrash() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8">
      <polyline points="3 4 13 4"/>
      <path d="M5 4V3a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v1"/>
      <path d="M6 7v5M10 7v5M4 4l1 9h6l1-9"/>
    </svg>
  )
}
function IconEye({ off }: { off?: boolean }) {
  return off ? (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path d="M2 2l12 12"/>
      <path d="M6.5 6.5A3 3 0 0 0 8 11a3 3 0 0 0 3-3"/>
      <path d="M14 8s-2.5 4-6 4c-.9 0-1.7-.2-2.4-.6"/>
      <path d="M2.7 5.3C1.6 6.3 1 8 1 8s3 4 7 4"/>
      <path d="M2 3.5C3.3 2.5 5.5 2 8 2c4 0 7 4 7 4s-.6 1.2-1.6 2.3"/>
    </svg>
  ) : (
    <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6">
      <path d="M1 8s3-5 7-5 7 5 7 5-3 5-7 5-7-5-7-5z"/>
      <circle cx="8" cy="8" r="2.5"/>
    </svg>
  )
}
function IconChevronRight() {
  return (
    <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
      <polyline points="6 4 10 8 6 12"/>
    </svg>
  )
}
function IconChevronLeft() {
  return (
    <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
      <polyline points="10 4 6 8 10 12"/>
    </svg>
  )
}
function IconChevronDown() {
  return (
    <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
      <polyline points="4 6 8 10 12 6"/>
    </svg>
  )
}
function IconGrip() {
  return (
    <svg width="8" height="13" viewBox="0 0 8 13" fill="currentColor">
      <circle cx="2" cy="2.5" r="1.2"/>
      <circle cx="6" cy="2.5" r="1.2"/>
      <circle cx="2" cy="6.5" r="1.2"/>
      <circle cx="6" cy="6.5" r="1.2"/>
      <circle cx="2" cy="10.5" r="1.2"/>
      <circle cx="6" cy="10.5" r="1.2"/>
    </svg>
  )
}
function IconFolder() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
      <path d="M1.5 3.5A1.5 1.5 0 0 1 3 2h3.5l1.5 2H13a1.5 1.5 0 0 1 1.5 1.5V13a1.5 1.5 0 0 1-1.5 1.5H3A1.5 1.5 0 0 1 1.5 13V3.5z"/>
    </svg>
  )
}

// ── Section label ────────────────────────────────────────────────
function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div style={{
      fontSize: 10, fontWeight: 700, textTransform: 'uppercase',
      letterSpacing: '0.09em', color: 'var(--text-3)',
      padding: '0 0 8px', marginBottom: 8,
      borderBottom: '1px solid var(--border)',
    }}>{children}</div>
  )
}

// ── Field row ─────────────────────────────────────────────────────
function FieldRow({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '160px 1fr', gap: '0 16px', alignItems: 'start', padding: '7px 0' }}>
      <div style={{ paddingTop: 8 }}>
        <div style={{ fontSize: 13, color: 'var(--text-2)' }}>{label}</div>
        {hint && <div style={{ fontSize: 11, color: 'var(--text-3)', marginTop: 2 }}>{hint}</div>}
      </div>
      <div>{children}</div>
    </div>
  )
}

// ── Provider detail panel ────────────────────────────────────────
function ProviderDetail({
  p, onUpdate,
}: {
  p: ProviderConfig
  onUpdate: (patch: Partial<ProviderConfig>) => void
}) {
  const [revealedKeys, setRevealedKeys] = useState<Set<number>>(new Set())
  const toggleReveal = (ki: number) =>
    setRevealedKeys(prev => {
      const next = new Set(prev)
      next.has(ki) ? next.delete(ki) : next.add(ki)
      return next
    })

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 28 }}>

      {/* Basic */}
      <div>
        <SectionLabel>基本设置</SectionLabel>
        <FieldRow label="名称">
          <input value={p.name} placeholder="sensenova" onChange={e => onUpdate({ name: e.target.value })} />
        </FieldRow>
        <FieldRow label="Max Retries">
          <input type="number" min={0} value={p.max_retries}
            onChange={e => onUpdate({ max_retries: parseInt(e.target.value) || 0 })}
            style={{ maxWidth: 80 }} />
        </FieldRow>
        <FieldRow label="Key 策略">
          <select value={p.key_strategy}
            onChange={e => onUpdate({ key_strategy: e.target.value as ProviderConfig['key_strategy'] })}
            style={{ maxWidth: 200 }}>
            <option value="fill-first">fill-first — 优先使用第一个可用 key</option>
            <option value="round-robin">round-robin — 轮询均摊</option>
          </select>
        </FieldRow>
      </div>

      {/* API Endpoints */}
      <div>
        <SectionLabel>API 接入点</SectionLabel>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {p.api.map((ep, ei) => (
            <div key={ei} style={{
              display: 'grid', gridTemplateColumns: '190px 1fr 32px',
              gap: 8, alignItems: 'center',
              padding: '8px 10px',
              background: 'var(--bg)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius)',
            }}>
              <select value={ep.api_format}
                onChange={e => onUpdate({
                  api: p.api.map((ep2, ej) => ej === ei ? { ...ep2, api_format: e.target.value as ApiFormat } : ep2),
                })}>
                {ALL_FORMATS.map(f => <option key={f} value={f}>{f}</option>)}
              </select>
              <input value={ep.base_url} placeholder="https://api.example.com/v1"
                style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
                onChange={e => onUpdate({
                  api: p.api.map((ep2, ej) => ej === ei ? { ...ep2, base_url: e.target.value } : ep2),
                })} />
              <button className="btn-icon" disabled={p.api.length <= 1}
                onClick={() => onUpdate({ api: p.api.filter((_, ej) => ej !== ei) })}>
                <IconTrash />
              </button>
            </div>
          ))}
          <button className="btn-add" onClick={() => onUpdate({ api: [...p.api, EMPTY_ENDPOINT()] })}>
            <IconPlus /> 添加接入点
          </button>
        </div>
      </div>

      {/* Keys */}
      <div>
        <SectionLabel>API Keys ({p.keys.length})</SectionLabel>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          {p.keys.map((k, ki) => {
            const isNew = k.key === ''
            const revealed = isNew || revealedKeys.has(ki)
            return (
              <div key={ki} style={{
                display: 'grid', gridTemplateColumns: '24px 1fr 32px 32px',
                gap: 6, alignItems: 'center',
                padding: '6px 10px',
                background: 'var(--bg)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius)',
              }}>
                <span style={{ fontSize: 11, color: 'var(--text-3)', fontFamily: 'var(--font-mono)', textAlign: 'right' }}>{ki + 1}</span>
                <input
                  type={revealed ? 'text' : 'password'}
                  value={k.key} placeholder="sk-..."
                  style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
                  onChange={e => onUpdate({
                    keys: p.keys.map((kk, kj) => kj === ki ? { key: e.target.value } : kk),
                  })} />
                <button className="btn-icon" title={revealed ? '隐藏' : '显示'}
                  onClick={() => toggleReveal(ki)}
                  style={{ color: revealed ? 'var(--accent)' : 'var(--text-3)' }}>
                  <IconEye off={revealed} />
                </button>
                <button className="btn-icon" disabled={p.keys.length <= 1}
                  onClick={() => {
                    onUpdate({ keys: p.keys.filter((_, kj) => kj !== ki) })
                    setRevealedKeys(prev => {
                      const next = new Set<number>()
                      prev.forEach(idx => { if (idx !== ki) next.add(idx > ki ? idx - 1 : idx) })
                      return next
                    })
                  }}>
                  <IconTrash />
                </button>
              </div>
            )
          })}
          <button className="btn-add" onClick={() => onUpdate({ keys: [...p.keys, { key: '' }] })}>
            <IconPlus /> 添加 Key
          </button>
        </div>
      </div>

      {/* Health check rules */}
      <div>
        <SectionLabel>健康检测规则 — 匹配时触发 key 轮换+冷却</SectionLabel>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {p.health_check_rules.map((r, ri) => (
            <div key={ri} style={{
              padding: 14, background: 'var(--bg)', border: '1px solid var(--border)',
              borderRadius: 'var(--radius)',
            }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
                <span style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-2)' }}>规则 {ri + 1}</span>
                <button className="btn-icon" onClick={() => onUpdate({
                  health_check_rules: p.health_check_rules.filter((_, rj) => rj !== ri),
                })}><IconTrash /></button>
              </div>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '8px 12px' }}>
                {[
                  { label: '描述', key: 'description', placeholder: 'quota_exceeded_flash', span: 2 },
                  { label: '冷却时长（秒）', key: 'cooldown_seconds', type: 'number' },
                  { label: 'JSONPath', key: 'jsonpath', placeholder: '$.error.type', mono: true, span: 1 },
                  { label: '匹配值', key: 'match_value', placeholder: 'quota_exceeded_error', span: 1 },
                ].map(f => (
                  <div key={f.key} style={{ gridColumn: f.span === 2 ? 'span 2' : undefined }}>
                    <div style={{ fontSize: 11, color: 'var(--text-3)', marginBottom: 4 }}>{f.label}</div>
                    <input
                      type={f.type || 'text'}
                      value={(r as unknown as Record<string, unknown>)[f.key] as string}
                      placeholder={f.placeholder}
                      style={f.mono ? { fontFamily: 'var(--font-mono)', fontSize: 12 } : undefined}
                      onChange={e => onUpdate({
                        health_check_rules: p.health_check_rules.map((rr, rj) =>
                          rj === ri ? { ...rr, [f.key]: f.type === 'number' ? parseInt(e.target.value) || 0 : e.target.value } : rr
                        ),
                      })} />
                  </div>
                ))}
                <div>
                  <div style={{ fontSize: 11, color: 'var(--text-3)', marginBottom: 4 }}>匹配方式</div>
                  <select value={r.match_type} onChange={e => onUpdate({
                    health_check_rules: p.health_check_rules.map((rr, rj) =>
                      rj === ri ? { ...rr, match_type: e.target.value as HealthCheckRule['match_type'] } : rr
                    ),
                  })}>
                    <option value="equals">equals</option>
                    <option value="contains">contains</option>
                    <option value="regex">regex</option>
                  </select>
                </div>
                <div style={{ gridColumn: 'span 2' }}>
                  <div style={{ fontSize: 11, color: 'var(--text-3)', marginBottom: 4 }}>
                    限定模型 <span style={{ color: 'var(--text-3)' }}>（逗号分隔，空=全部）</span>
                  </div>
                  <input value={r.models.join(',')} placeholder="deepseek-v4-flash,..."
                    onChange={e => onUpdate({
                      health_check_rules: p.health_check_rules.map((rr, rj) =>
                        rj === ri ? { ...rr, models: e.target.value.split(',').map(s => s.trim()).filter(Boolean) } : rr
                      ),
                    })} />
                </div>
                <div style={{ gridColumn: 'span 2' }}>
                  <div style={{ fontSize: 11, color: 'var(--text-3)', marginBottom: 4 }}>
                    HTTP 状态码 <span style={{ color: 'var(--text-3)' }}>（逗号分隔，如 429, 500；空=仅按 JSONPath 匹配）</span>
                  </div>
                  <input value={(r.http_status_codes ?? []).join(',')} placeholder="429, 500, 502, 503"
                    onChange={e => onUpdate({
                      health_check_rules: p.health_check_rules.map((rr, rj) =>
                        rj === ri ? {
                          ...rr,
                          http_status_codes: e.target.value.split(',').map(s => parseInt(s.trim(), 10)).filter(n => !isNaN(n)),
                        } : rr
                      ),
                    })} />
                </div>
              </div>
            </div>
          ))}
          <button className="btn-add" onClick={() => onUpdate({
            health_check_rules: [...p.health_check_rules, EMPTY_RULE()],
          })}>
            <IconPlus /> 添加规则
          </button>
        </div>
      </div>

      {/* Per-provider proxy */}
      <div>
        <SectionLabel>网络代理（Provider 级别）</SectionLabel>
        <FieldRow label="代理策略" hint="优先级高于全局代理设置">
          <ProviderProxyField
            proxy={p.proxy}
            onChange={proxy => onUpdate({ proxy })}
          />
        </FieldRow>
      </div>
    </div>
  )
}

// ── Combo detail panel ───────────────────────────────────────────
// ── Combo member row with models.dev metadata ───────────────────
const COST_FIELDS: { key: keyof ModelCost; label: string }[] = [
  { key: 'input', label: '输入 $/1M' },
  { key: 'output', label: '输出 $/1M' },
  { key: 'cache_read', label: '缓存读 $/1M' },
  { key: 'cache_write', label: '缓存写 $/1M' },
]

function ComboMemberRow({
  member, index, total, providerNames, onChange, onRemove, onMoveUp, onMoveDown,
}: {
  member: ComboMember
  index: number
  total: number
  providerNames: string[]
  onChange: (next: ComboMember) => void
  onRemove: () => void
  onMoveUp: () => void
  onMoveDown: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [pickerOpen, setPickerOpen] = useState(false)
  const [suggesting, setSuggesting] = useState(false)
  const [suggestMsg, setSuggestMsg] = useState('')
  const [candidates, setCandidates] = useState<ModelsDevEntry[]>([])

  const hasParams = member.context_window != null || member.max_output_tokens != null || member.cost != null
  const patch = (next: Partial<ComboMember>) => onChange({ ...member, ...next })
  const num = (v: string): number | undefined => (v === '' ? undefined : Number(v))

  // Snapshot the catalog entry into the member. Undefined fields are omitted
  // so the config dump stays clean; explicit zeros are preserved.
  const applyEntry = (e: ModelsDevEntry) => {
    const cost: ModelCost | undefined = e.cost ? { ...e.cost } : undefined
    onChange({
      ...member,
      context_window: e.context_window,
      max_output_tokens: e.max_output_tokens,
      cost,
      models_dev_source: e.source_id,
    })
    setCandidates([])
    setSuggestMsg(`已从 models.dev 填充（${e.source_id}）`)
    setExpanded(true)
  }

  const suggest = async () => {
    if (!member.model.trim()) return
    setSuggesting(true)
    setSuggestMsg('')
    try {
      const res = await suggestModelsDev({
        model: member.model.trim(),
        provider: member.provider || undefined,
        limit: 5,
      })
      const top = res.items[0]
      if (!top) {
        setSuggestMsg(res.status.ready
          ? 'models.dev 未找到匹配，可手动填写'
          : 'models.dev 目录正在加载，请稍后重试')
        return
      }
      const second = res.items[1]
      const topScore = top.match_score ?? 0
      if (topScore >= 0.9 && (!second || topScore > (second.match_score ?? 0))) {
        applyEntry(top)
      } else {
        setCandidates(res.items)
        setSuggestMsg(`找到 ${res.items.length} 个候选，请选择`)
      }
    } catch (e: unknown) {
      setSuggestMsg(String(e))
    }
    setSuggesting(false)
  }

  const clearParams = () => {
    onChange({
      ...member,
      context_window: undefined,
      max_output_tokens: undefined,
      cost: undefined,
      models_dev_source: undefined,
    })
    setCandidates([])
    setSuggestMsg('')
  }

  const smallBtn = { fontSize: 11, padding: '2px 8px' }

  return (
    <div style={{
      background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: 'var(--radius)',
      padding: '8px 10px', display: 'flex', flexDirection: 'column', gap: 6,
    }}>
      <div style={{ display: 'grid', gridTemplateColumns: '24px 1fr 1fr 180px 84px', gap: 8, alignItems: 'center' }}>
        <span style={{ fontSize: 11, color: 'var(--text-3)', fontFamily: 'var(--font-mono)', textAlign: 'center' }}>{index + 1}</span>
        <select value={member.provider}
          onChange={e => patch({ provider: e.target.value })}>
          <option value="">— Provider —</option>
          {providerNames.map(n => <option key={n} value={n}>{n}</option>)}
        </select>
        <input value={member.model} placeholder="上游模型 ID"
          style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
          onChange={e => patch({ model: e.target.value })} />
        {/* upstream_api_format: empty = same as combo's client format (no translation) */}
        <select
          value={member.upstream_api_format ?? ''}
          title="留空表示与客户端格式相同（无需翻译）"
          onChange={e => patch({ upstream_api_format: e.target.value || undefined })}
          style={{ fontSize: 12, color: member.upstream_api_format ? 'var(--text)' : 'var(--text-3)' }}
        >
          <option value="">— 同客户端格式 —</option>
          {ALL_FORMATS.map(f => <option key={f} value={f}>{f}</option>)}
        </select>
        <div style={{ display: 'flex', alignItems: 'center', gap: 2 }}>
          <button type="button" className="btn-icon" disabled={index === 0} title="上移"
            onClick={onMoveUp} style={{ width: 22, height: 22, padding: 0, fontSize: 11 }}>
            ↑
          </button>
          <button type="button" className="btn-icon" disabled={index === total - 1} title="下移"
            onClick={onMoveDown} style={{ width: 22, height: 22, padding: 0, fontSize: 11 }}>
            ↓
          </button>
          <button type="button" className="btn-icon" disabled={total <= 1} title="删除"
            onClick={onRemove} style={{ width: 22, height: 22, padding: 0 }}>
            <IconTrash />
          </button>
        </div>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', fontSize: 11, color: 'var(--text-3)' }}>
        <button type="button" className="btn btn-sm" style={smallBtn} onClick={() => setExpanded(v => !v)}>
          {expanded ? '收起参数' : `模型参数与价格${hasParams ? ' ●' : ''}`}
        </button>
        <button type="button" className="btn btn-sm" style={smallBtn}
          onClick={suggest} disabled={suggesting || !member.model.trim()}>
          {suggesting ? '查询中…' : '自动获取'}
        </button>
        <button type="button" className="btn btn-sm" style={smallBtn} onClick={() => setPickerOpen(true)}>
          从 models.dev 选择
        </button>
        {member.models_dev_source && <code style={{ fontSize: 11 }}>{member.models_dev_source}</code>}
        {hasParams && (
          <button type="button" className="btn btn-sm" style={smallBtn} onClick={clearParams}>清除参数</button>
        )}
        {suggestMsg && <span style={{ color: 'var(--text-2)' }}>{suggestMsg}</span>}
      </div>

      {candidates.length > 0 && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4, fontSize: 12 }}>
          <span style={{ color: 'var(--text-3)' }}>候选（价格可能不同）：</span>
          {candidates.map(c => (
            <button key={c.source_id} type="button" className="btn btn-sm"
              style={{ justifyContent: 'flex-start', textAlign: 'left', fontSize: 12 }}
              onClick={() => applyEntry(c)}>
              {c.provider_name} / {c.model_id}
              {c.context_window != null ? ` · ${c.context_window.toLocaleString()} ctx` : ''}
              {c.cost?.input != null ? ` · $${c.cost.input}/1M in` : ''}
              {c.cost?.output != null ? ` · $${c.cost.output}/1M out` : ''}
            </button>
          ))}
        </div>
      )}

      {expanded && (
        <div style={{
          display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 8,
          paddingTop: 8, borderTop: '1px solid var(--border)',
        }}>
          <label style={{ fontSize: 11, color: 'var(--text-3)', display: 'flex', flexDirection: 'column', gap: 3 }}>
            Context Window (tokens)
            <input type="number" min={1} value={member.context_window ?? ''}
              onChange={e => patch({ context_window: num(e.target.value) })} />
          </label>
          <label style={{ fontSize: 11, color: 'var(--text-3)', display: 'flex', flexDirection: 'column', gap: 3 }}>
            Max Output Tokens
            <input type="number" min={1} value={member.max_output_tokens ?? ''}
              onChange={e => patch({ max_output_tokens: num(e.target.value) })} />
          </label>
          {COST_FIELDS.map(f => (
            <label key={f.key} style={{ fontSize: 11, color: 'var(--text-3)', display: 'flex', flexDirection: 'column', gap: 3 }}>
              {f.label}
              <input type="number" min={0} step="any" value={member.cost?.[f.key] ?? ''}
                onChange={e => patch({ cost: { ...(member.cost ?? {}), [f.key]: num(e.target.value) } })} />
            </label>
          ))}
        </div>
      )}

      <ModelsDevPicker
        open={pickerOpen}
        initialQuery={member.model}
        providerHint={member.provider}
        onClose={() => setPickerOpen(false)}
        onSelect={applyEntry}
      />
    </div>
  )
}

function ComboDetail({
  cb, providerNames, existingGroups, onUpdate,
}: {
  cb: ComboConfig
  providerNames: string[]
  existingGroups: string[]
  onUpdate: (patch: Partial<ComboConfig>) => void
}) {
  const formats = normalizeFormats(cb.api_format)
  const isDefault = cb.is_default || cb.owned_by === 'default' || !cb.owned_by
  const fullPrimary = isDefault ? (cb.name || 'fast') : `${cb.owned_by || 'default'}/${cb.name || 'fast'}`

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 28 }}>
      {/* Basic */}
      <div>
        <SectionLabel>基本设置</SectionLabel>
        <FieldRow label="名称" hint="Combo 名称（如 fast, smart 等）">
          <input value={cb.name} placeholder="fast" onChange={e => onUpdate({ name: e.target.value })} />
        </FieldRow>
        <FieldRow label="所属分组 (owned_by)" hint="分组 ID。留空或 default 为默认分组；非默认分组在 /v1/models 及请求时使用 <owned_by>/<name>">
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
              <input
                list="owned-by-options"
                value={cb.owned_by ?? ''}
                placeholder="default"
                style={{ maxWidth: 220 }}
                onChange={e => {
                  const val = e.target.value.trim()
                  onUpdate({
                    owned_by: val,
                    is_default: val === '' || val === 'default' ? true : cb.is_default,
                  })
                }}
              />
              <datalist id="owned-by-options">
                {existingGroups.map(g => (
                  <option key={g} value={g} />
                ))}
              </datalist>
              <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={Boolean(cb.is_default)}
                  onChange={e => onUpdate({ is_default: e.target.checked })}
                />
                设为默认分组 (无前缀)
              </label>
            </div>
            {/* Quick group chips */}
            {existingGroups.length > 0 && (
              <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                <span style={{ fontSize: 11, color: 'var(--text-3)' }}>快捷归组:</span>
                {existingGroups.map(g => {
                  const active = (cb.owned_by || 'default') === g
                  return (
                    <button
                      key={g}
                      type="button"
                      onClick={() => onUpdate({
                        owned_by: g,
                        is_default: g === 'default' ? true : cb.is_default,
                      })}
                      style={{
                        fontSize: 11, padding: '2px 8px', borderRadius: 4, cursor: 'pointer',
                        border: `1px solid ${active ? 'var(--accent)' : 'var(--border)'}`,
                        background: active ? 'var(--accent-light)' : 'var(--bg-input)',
                        color: active ? 'var(--accent)' : 'var(--text-2)',
                        fontWeight: active ? 600 : 400,
                      }}
                    >
                      {g}
                    </button>
                  )
                })}
              </div>
            )}
            <div style={{ fontSize: 11, color: 'var(--text-3)' }}>
              完整调用标识 Preview：
              <code style={{
                fontFamily: 'var(--font-mono)', fontSize: 12, fontWeight: 600,
                color: 'var(--accent)', background: 'var(--accent-light)',
                padding: '2px 6px', borderRadius: 4, marginLeft: 4,
              }}>{fullPrimary}</code>
            </div>
          </div>
        </FieldRow>
        <FieldRow label="别名" hint="其他可用的 model ID，逗号分隔">
          <input
            value={(cb.aliases ?? []).join(', ')}
            placeholder="gpt-4o, claude-3-5-sonnet-20241022"
            onChange={e => {
              const raw = e.target.value
              const aliases = raw.split(',').map(s => s.trim()).filter(Boolean)
              onUpdate({ aliases })
            }}
          />
        </FieldRow>
        <FieldRow label="策略">
          <select value={cb.strategy}
            onChange={e => onUpdate({ strategy: e.target.value as ComboConfig['strategy'] })}
            style={{ maxWidth: 260 }}>
            <option value="fill-first">fill-first — 优先第一个 member，耗尽才切换</option>
            <option value="round-robin">round-robin — 每次请求轮换 member</option>
          </select>
        </FieldRow>
      </div>

      {/* API formats */}
      <div>
        <SectionLabel>接受的 API 格式（客户端可使用的格式）</SectionLabel>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {CLIENT_FORMATS.map(f => {
            const checked = formats.includes(f)
            return (
              <label key={f} style={{
                display: 'flex', alignItems: 'center', gap: 10,
                padding: '9px 12px',
                border: `1px solid ${checked ? 'var(--accent)' : 'var(--border-md)'}`,
                borderRadius: 'var(--radius)',
                background: checked ? 'var(--accent-light)' : 'var(--bg-input)',
                cursor: 'pointer',
                transition: 'all 0.12s',
              }}
                onClick={e => {
                  e.preventDefault()
                  const next = checked ? formats.filter(x => x !== f) : [...formats, f]
                  onUpdate({ api_format: next.length > 0 ? next : formats })
                }}>
                <input type="checkbox" checked={checked} onChange={() => {}} />
                <span style={{
                  fontFamily: 'var(--font-mono)', fontSize: 12,
                  color: checked ? 'var(--accent)' : 'var(--text)',
                  fontWeight: checked ? 500 : 400,
                }}>{f}</span>
                <span style={{ fontSize: 11, color: checked ? 'var(--accent-dim)' : 'var(--text-3)', marginLeft: 4 }}>
                  {FMT_ENDPOINT[f]}
                </span>
              </label>
            )
          })}
        </div>
      </div>

      {/* Members */}
      <div>
        <SectionLabel>Members — 按策略顺序选用</SectionLabel>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          {/* Column header */}
          <div style={{
            display: 'grid', gridTemplateColumns: '24px 1fr 1fr 180px 84px',
            gap: 8, padding: '0 10px', marginBottom: 2,
          }}>
            {['#', 'Provider', '上游模型 ID', '上游 API 格式（可选）', '操作'].map((h, i) => (
              <span key={i} style={{ fontSize: 11, color: 'var(--text-3)' }}>{h}</span>
            ))}
          </div>
          {cb.members.map((m, mi) => (
            <ComboMemberRow
              key={mi}
              member={m}
              index={mi}
              total={cb.members.length}
              providerNames={providerNames}
              onChange={next => onUpdate({
                members: cb.members.map((mm, mj): ComboMember => mj === mi ? next : mm),
              })}
              onRemove={() => onUpdate({ members: cb.members.filter((_, mj) => mj !== mi) })}
              onMoveUp={() => {
                const members = [...cb.members]
                ;[members[mi - 1], members[mi]] = [members[mi], members[mi - 1]]
                onUpdate({ members })
              }}
              onMoveDown={() => {
                const members = [...cb.members]
                ;[members[mi + 1], members[mi]] = [members[mi], members[mi + 1]]
                onUpdate({ members })
              }}
            />
          ))}
          {/* Translation note */}
          <div style={{ fontSize: 11, color: 'var(--text-3)', padding: '4px 10px', lineHeight: 1.5 }}>
            设置「上游 API 格式」后，ccrouter 会自动通过 CLIProxyAPI translator 在客户端格式与上游格式之间转换请求和响应。
            例如：combo 接受 <code style={{ fontFamily: 'var(--font-mono)' }}>anthropic</code>，但 provider 只有 <code style={{ fontFamily: 'var(--font-mono)' }}>openai</code> 接口 → 设为 <code style={{ fontFamily: 'var(--font-mono)' }}>openai</code> 即可自动翻译。
          </div>
          <button className="btn-add"
            onClick={() => onUpdate({ members: [...cb.members, { provider: '', model: '' }] })}>
            <IconPlus /> 添加 Member
          </button>
        </div>
      </div>
    </div>
  )
}

// ── Payload rule detail panel ────────────────────────────────────
// ── General settings panel ───────────────────────────────────────
function GeneralPanel({
  general,
  logging,
  onUpdate,
  onUpdateLogging,
}: {
  general: GeneralConfig
  logging: LoggingConfig
  onUpdate: (patch: Partial<GeneralConfig>) => void
  onUpdateLogging: (patch: Partial<LoggingConfig>) => void
}) {
  const [revealedKeys, setRevealedKeys] = useState<Set<number>>(new Set())
  const [revealAdminPw, setRevealAdminPw] = useState(false)
  const toggleReveal = (ki: number) =>
    setRevealedKeys(prev => {
      const next = new Set(prev)
      next.has(ki) ? next.delete(ki) : next.add(ki)
      return next
    })

  const apiKeys = general.api_keys ?? []
  const proxy = general.proxy ?? {}
  const modelsDev = general.models_dev ?? {}

  const updateProxy = (patch: Partial<ProxyConfig>) =>
    onUpdate({ proxy: { ...proxy, ...patch } })
  const updateModelsDev = (patch: Partial<ModelsDevConfig>) =>
    onUpdate({ models_dev: { ...modelsDev, ...patch } })

  const proxyEnabled = !!(proxy.url || proxy.disabled !== undefined)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 32, maxWidth: 720 }}>

      {/* Admin Password */}
      <div>
        <SectionLabel>管理页面认证密码</SectionLabel>
        <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 12 }}>
          设置后，访问 Web 管理控制台及调用管理 API（/admin/api/*）必须进行密码认证。留空表示不启用认证（完全公开）。
        </div>
        <FieldRow label="管理员密码" hint="留空 = 不启用管理认证">
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, maxWidth: 360, width: '100%' }}>
            <input
              type={revealAdminPw ? 'text' : 'password'}
              value={general.admin_password ?? ''}
              placeholder="留空表示无需登录认证"
              style={{ fontFamily: 'var(--font-mono)', fontSize: 12, flex: 1 }}
              onChange={e => {
                const val = e.target.value
                onUpdate({ admin_password: val || undefined })
              }}
            />
            <button
              className="btn-icon"
              title={revealAdminPw ? '隐藏' : '显示'}
              onClick={() => setRevealAdminPw(prev => !prev)}
              style={{ color: revealAdminPw ? 'var(--accent)' : 'var(--text-3)' }}
            >
              <IconEye off={revealAdminPw} />
            </button>
            {general.admin_password && (
              <button
                className="btn-icon"
                title="清除密码"
                onClick={() => onUpdate({ admin_password: undefined })}
                style={{ color: 'var(--text-3)' }}
              >
                <IconTrash />
              </button>
            )}
          </div>
        </FieldRow>
        {general.admin_password && (
          <div style={{ marginTop: 4, fontSize: 12, color: 'var(--ok-fg)' }}>
            ✓ 已启用管理页面密码认证
          </div>
        )}
      </div>

      {/* API Keys */}
      <div>
        <SectionLabel>API 密钥（客户端访问认证）</SectionLabel>
        <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 12 }}>
          配置后客户端必须携带以下任一请求头才能访问代理接口：
          <code style={{ fontFamily: 'var(--font-mono)', margin: '0 4px' }}>Authorization: Bearer &lt;key&gt;</code>（OpenAI / 通用）
          或 <code style={{ fontFamily: 'var(--font-mono)', margin: '0 4px' }}>X-Api-Key: &lt;key&gt;</code>（Anthropic SDK）。
          留空则不启用认证。
        </div>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
          {apiKeys.map((k, ki) => {
            const isNew = k.key === ''
            const revealed = isNew || revealedKeys.has(ki)
            return (
              <div key={ki} style={{
                display: 'grid', gridTemplateColumns: '24px 1fr 32px 32px',
                gap: 6, alignItems: 'center',
                padding: '6px 10px',
                background: 'var(--bg)',
                border: '1px solid var(--border)',
                borderRadius: 'var(--radius)',
              }}>
                <span style={{ fontSize: 11, color: 'var(--text-3)', fontFamily: 'var(--font-mono)', textAlign: 'right' }}>{ki + 1}</span>
                <input
                  type={revealed ? 'text' : 'password'}
                  value={k.key}
                  placeholder="sk-..."
                  style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
                  onChange={e => onUpdate({
                    api_keys: apiKeys.map((kk, kj) => kj === ki ? { key: e.target.value } : kk),
                  })}
                />
                <button className="btn-icon" title={revealed ? '隐藏' : '显示'}
                  onClick={() => toggleReveal(ki)}
                  style={{ color: revealed ? 'var(--accent)' : 'var(--text-3)' }}>
                  <IconEye off={revealed} />
                </button>
                <button className="btn-icon"
                  onClick={() => onUpdate({ api_keys: apiKeys.filter((_, kj) => kj !== ki) })}>
                  <IconTrash />
                </button>
              </div>
            )
          })}
          <button className="btn-add" onClick={() => onUpdate({ api_keys: [...apiKeys, { key: '' }] })}>
            <IconPlus /> 添加密钥
          </button>
        </div>
      </div>

      {/* Global Proxy */}
      <div>
        <SectionLabel>网络代理（全局出站代理）</SectionLabel>
        <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 12 }}>
          所有 Provider 默认走此代理。支持 <code style={{ fontFamily: 'var(--font-mono)' }}>http://</code>、
          <code style={{ fontFamily: 'var(--font-mono)' }}>https://</code>、
          <code style={{ fontFamily: 'var(--font-mono)' }}>socks5://</code> 格式。
          各 Provider 可单独覆盖（使用自己的代理或禁用代理）。
        </div>
        <FieldRow label="代理地址" hint="留空则不走代理">
          <input
            value={proxy.url ?? ''}
            placeholder="socks5://127.0.0.1:7890 或 http://127.0.0.1:8080"
            style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
            onChange={e => {
              const val = e.target.value.trim()
              updateProxy({ url: val || undefined, disabled: undefined })
            }}
          />
        </FieldRow>
        {proxyEnabled && (
          <div style={{ marginTop: 4, fontSize: 12, color: proxy.url ? 'var(--ok-fg)' : 'var(--text-3)' }}>
            {proxy.url ? `✓ 全局代理已设置` : '代理地址为空，全局代理未启用'}
          </div>
        )}
      </div>

      {/* Request Timeout */}
      <div>
        <SectionLabel>请求超时设置</SectionLabel>
        <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 12 }}>
          每个上游请求的总超时时间。超时后网关会返回 504 错误并尝试重试（如果有备用 key 或 member）。
          设置为 <code>0</code> 表示禁用超时（无限等待），留空则使用默认值。
        </div>
        <FieldRow label="超时时间（秒）" hint="留空 = 默认 600 秒（10 分钟）；0 = 禁用超时">
          <input
            type="number"
            min={0}
            placeholder="600"
            value={general.request_timeout_seconds ?? ''}
            onChange={e => {
              const raw = e.target.value
              if (raw === '') {
                onUpdate({ request_timeout_seconds: undefined })
                return
              }
              const val = parseInt(raw, 10)
              onUpdate({ request_timeout_seconds: isNaN(val) || val < 0 ? undefined : val })
            }}
            style={{ maxWidth: 120 }}
          />
        </FieldRow>
      </div>

      {/* Verbose Logging & Storage */}
      <div>
        <SectionLabel>详细请求日志与存储配置</SectionLabel>
        <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 12 }}>
          记录包含完整请求体与响应体的详细日志，使用 zstd 字典链压缩存储于本地磁盘（requests.zrc）。
          <span style={{ color: 'var(--warn-fg)', marginLeft: 6 }}>
            ⚠ 报文含明文 API 密钥，仅限本地或受保护环境使用。
          </span>
        </div>

        <FieldRow label="详细记录" hint="是否启用请求报文全量落盘记录">
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
            <input
              type="checkbox"
              checked={logging.enabled ?? false}
              onChange={e => onUpdateLogging({ enabled: e.target.checked })}
            />
            <span style={{ fontSize: 13, fontWeight: 500 }}>
              {logging.enabled ? '已启用详细记录' : '未启用详细记录'}
            </span>
          </label>
        </FieldRow>

        <FieldRow label="存储路径 (Dir)" hint="日志切片文件存放目录，默认 logs">
          <input
            value={logging.dir ?? ''}
            placeholder="logs"
            style={{ fontFamily: 'var(--font-mono)', fontSize: 12, maxWidth: 300 }}
            onChange={e => onUpdateLogging({ dir: e.target.value.trim() || undefined })}
          />
        </FieldRow>

        <FieldRow label="单文件上限 (MB)" hint="单个切片文件大小上限，达到后自动轮转，默认 20MB">
          <input
            type="number"
            min={1}
            max={2048}
            value={logging.max_file_size_mb ?? ''}
            placeholder="20"
            style={{ fontFamily: 'var(--font-mono)', fontSize: 12, maxWidth: 120 }}
            onChange={e => onUpdateLogging({ max_file_size_mb: parseOptionalInt(e.target.value) })}
          />
        </FieldRow>

        <FieldRow label="保留文件数 (Backups)" hint="超出上限的最旧切片文件将被删除，默认 10 个">
          <input
            type="number"
            min={1}
            max={100}
            value={logging.max_backups ?? ''}
            placeholder="10"
            style={{ fontFamily: 'var(--font-mono)', fontSize: 12, maxWidth: 120 }}
            onChange={e => onUpdateLogging({ max_backups: parseOptionalInt(e.target.value) })}
          />
        </FieldRow>

        <FieldRow label="压缩等级 (Level)" hint="zstd 压缩等级，默认 best（极致压缩）">
          <select
            value={logging.compression_level ?? 'best'}
            style={{ fontSize: 12, maxWidth: 200 }}
            onChange={e => onUpdateLogging({ compression_level: e.target.value })}
          >
            <option value="fastest">fastest（极速）</option>
            <option value="default">default（标准）</option>
            <option value="better">better（高压缩比）</option>
            <option value="best">best（极致压缩）</option>
          </select>
        </FieldRow>
      </div>

      {/* models.dev catalog */}
      <div>
        <SectionLabel>models.dev 模型目录</SectionLabel>
        <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 12 }}>
          用于在 Combo 成员编辑时搜索模型并自动填充上下文长度与价格。选中的值会快照到成员配置，
          因此 models.dev 不可用时不影响代理与计费。
        </div>
        <FieldRow label="启用自动获取" hint="关闭后管理界面不再请求 models.dev，仍可手动填写参数">
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 8, cursor: 'pointer' }}>
            <input
              type="checkbox"
              checked={!modelsDev.disabled}
              onChange={e => updateModelsDev({ disabled: e.target.checked ? undefined : true })}
            />
            <span style={{ fontSize: 13, fontWeight: 500 }}>
              {modelsDev.disabled ? '已禁用' : '已启用'}
            </span>
          </label>
        </FieldRow>
        <FieldRow label="Base URL" hint="默认 https://models.dev，可指向本地镜像">
          <input
            value={modelsDev.base_url ?? ''}
            placeholder="https://models.dev"
            style={{ fontFamily: 'var(--font-mono)', fontSize: 12, maxWidth: 360 }}
            onChange={e => updateModelsDev({ base_url: e.target.value.trim() || undefined })}
          />
        </FieldRow>
        <FieldRow label="请求超时（秒）" hint="拉取目录的超时时间，默认 10">
          <input
            type="number" min={1} max={600}
            value={modelsDev.timeout_seconds ?? ''}
            placeholder="10"
            style={{ fontFamily: 'var(--font-mono)', fontSize: 12, maxWidth: 120 }}
            onChange={e => updateModelsDev({ timeout_seconds: parseOptionalInt(e.target.value) })}
          />
        </FieldRow>
        <FieldRow label="缓存有效期（秒）" hint="目录缓存时间，默认 3600">
          <input
            type="number" min={1} max={86400}
            value={modelsDev.cache_ttl_seconds ?? ''}
            placeholder="3600"
            style={{ fontFamily: 'var(--font-mono)', fontSize: 12, maxWidth: 120 }}
            onChange={e => updateModelsDev({ cache_ttl_seconds: parseOptionalInt(e.target.value) })}
          />
        </FieldRow>
      </div>
    </div>
  )
}

// ── Per-provider proxy field ──────────────────────────────────────
function ProviderProxyField({
  proxy,
  onChange,
}: {
  proxy: ProxyConfig | undefined
  onChange: (p: ProxyConfig | undefined) => void
}) {
  const mode = proxy?.disabled ? 'disabled' : (proxy && 'url' in proxy) ? 'custom' : 'inherit'

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
      <div style={{ display: 'flex', gap: 6 }}>
        {([
          ['inherit', '跟随全局'] as const,
          ['custom', '自定义代理'] as const,
          ['disabled', '不走代理'] as const,
        ]).map(([val, label]) => (
          <label key={val} onClick={() => {
            if (val === 'inherit') onChange(undefined)
            else if (val === 'disabled') onChange({ disabled: true })
            else onChange({ url: '' })
          }} style={{
            display: 'flex', alignItems: 'center', gap: 5,
            padding: '5px 10px', borderRadius: 5, cursor: 'pointer',
            border: `1px solid ${mode === val ? 'var(--accent)' : 'var(--border-md)'}`,
            background: mode === val ? 'var(--accent-light)' : 'var(--bg-input)',
            fontSize: 12, fontWeight: mode === val ? 600 : 400, userSelect: 'none',
          }}>
            {label}
          </label>
        ))}
      </div>
      {mode === 'custom' && (
        <input
          value={proxy?.url ?? ''}
          placeholder="socks5://127.0.0.1:7890 或 http://127.0.0.1:8080"
          style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
          onChange={e => onChange({ url: e.target.value || undefined })}
        />
      )}
      {mode === 'disabled' && (
        <div style={{ fontSize: 12, color: 'var(--warn-fg)' }}>此 Provider 的请求不走任何代理</div>
      )}
    </div>
  )
}

// ── Main ─────────────────────────────────────────────────────────
export default function ConfigEditor() {
  const [cfg, setCfg] = useState<AppConfig | null>(null)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [tab, setTab] = useState<'general' | 'providers' | 'combos' | 'payload'>('general')
  const [selProvider, setSelProvider] = useState(0)
  const [selCombo, setSelCombo] = useState(0)
  const [selPayload, setSelPayload] = useState(0)
  const [sideListCollapsed, setSideListCollapsed] = useState(false)
  const [draggedComboIndex, setDraggedComboIndex] = useState<number | null>(null)
  const [dragOverComboIndex, setDragOverComboIndex] = useState<number | null>(null)
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(new Set())

  // Group combos by owned_by
  const groupedCombos = useMemo(() => {
    if (!cfg) return []
    const map = new Map<string, { ownedBy: string; isDefault: boolean; combos: { combo: ComboConfig; globalIndex: number }[] }>()

    cfg.combos.forEach((cb, globalIndex) => {
      const groupKey = (cb.owned_by || 'default').trim()
      const isDef = Boolean(cb.is_default || groupKey === 'default')
      if (!map.has(groupKey)) {
        map.set(groupKey, { ownedBy: groupKey, isDefault: isDef, combos: [] })
      }
      const g = map.get(groupKey)!
      if (cb.is_default) g.isDefault = true
      g.combos.push({ combo: cb, globalIndex })
    })

    const list = Array.from(map.values())
    list.sort((a, b) => {
      if (a.isDefault && !b.isDefault) return -1
      if (!a.isDefault && b.isDefault) return 1
      return a.ownedBy.localeCompare(b.ownedBy)
    })
    return list
  }, [cfg?.combos])

  const existingGroups = useMemo(() => {
    const set = new Set<string>(['default'])
    if (cfg) {
      cfg.combos.forEach(c => {
        if (c.owned_by) set.add(c.owned_by.trim())
      })
    }
    return Array.from(set)
  }, [cfg?.combos])

  useEffect(() => {
    getConfig()
      .then(raw => {
        let flatCombos: ComboConfig[] = []
        if (Array.isArray(raw.combos)) {
          for (const item of raw.combos as any[]) {
            if (Array.isArray(item.combos)) {
              const groupOwnedBy = item.owned_by || 'default'
              const groupDefault = Boolean(item.default || item.is_default)
              for (const inner of item.combos) {
                flatCombos.push({
                  aliases: [],
                  ...inner,
                  owned_by: inner.owned_by || groupOwnedBy,
                  is_default: inner.is_default ?? groupDefault,
                  api_format: normalizeFormats(inner.api_format),
                  members: (inner.members ?? []).map((m: any) => ({ ...m, upstream_api_format: m.upstream_api_format ?? '' })),
                })
              }
            } else {
              flatCombos.push({
                aliases: [],
                ...item,
                owned_by: item.owned_by || 'default',
                is_default: item.is_default ?? (item.owned_by === 'default' || !item.owned_by),
                api_format: normalizeFormats(item.api_format),
                members: (item.members ?? []).map((m: any) => ({ ...m, upstream_api_format: m.upstream_api_format ?? '' })),
              })
            }
          }
        }
        setCfg({
          ...raw,
          general: {
            ...EMPTY_GENERAL(),
            ...raw.general,
            api_keys: raw.general?.api_keys ?? [],
          },
          combos: flatCombos,
          payload_scripts: raw.payload_scripts ?? [],
        })
      })
      .catch(e => setErr(String(e)))
  }, [])

  if (!cfg) return (
    <div className="page">
      {err ? <div className="alert err">{err}</div> : <div className="empty-state">加载中…</div>}
    </div>
  )

  const save = async () => {
    setSaving(true); setMsg(''); setErr('')
    try {
      // Clean up general: strip empty api_keys, strip proxy if no url and not disabled
      const general = cfg.general ?? {}
      const cleanGeneral: typeof general = {
        ...general,
        api_keys: (general.api_keys ?? []).filter(k => k.key.trim() !== ''),
      }
      if (!cleanGeneral.proxy?.url && !cleanGeneral.proxy?.disabled) {
        delete cleanGeneral.proxy
      }
      if ((cleanGeneral.api_keys ?? []).length === 0) {
        delete cleanGeneral.api_keys
      }
      if (!cleanGeneral.admin_password || cleanGeneral.admin_password.trim() === '') {
        delete cleanGeneral.admin_password
      }
      if (cleanGeneral.models_dev) {
        const md = { ...cleanGeneral.models_dev }
        if (!md.disabled) delete md.disabled
        if (!md.base_url) delete md.base_url
        if (md.timeout_seconds == null) delete md.timeout_seconds
        if (md.cache_ttl_seconds == null) delete md.cache_ttl_seconds
        if (Object.keys(md).length === 0) delete cleanGeneral.models_dev
        else cleanGeneral.models_dev = md
      }

      await putConfig({
        ...cfg,
        general: cleanGeneral,
        combos: cfg.combos.map(c => ({
          ...c,
          owned_by: c.owned_by || 'default',
          is_default: Boolean(c.is_default),
          api_format: (c.api_format as ApiFormat[]).length === 1
            ? (c.api_format as ApiFormat[])[0]
            : c.api_format,
          members: c.members.map(m => {
            const mm = { ...m }
            if (!mm.upstream_api_format) delete mm.upstream_api_format
            if (mm.context_window == null) delete mm.context_window
            if (mm.max_output_tokens == null) delete mm.max_output_tokens
            if (mm.cost) {
              const cost = { ...mm.cost }
              COST_FIELDS.forEach(f => {
                if (cost[f.key] == null) delete cost[f.key]
              })
              if (Object.keys(cost).length === 0) delete mm.cost
              else mm.cost = cost
            } else {
              delete mm.cost
            }
            if (!mm.models_dev_source) delete mm.models_dev_source
            return mm
          }),
        })),
        providers: cfg.providers.map(p => {
          const pp = { ...p }
          // Clean up per-provider proxy
          if (!pp.proxy?.url && !pp.proxy?.disabled) {
            delete pp.proxy
          }
          return pp
        }),
      })
      setMsg('配置已保存并热重载')
    } catch (e: unknown) { setErr(String(e)) }
    setSaving(false)
  }

  const updateGeneral = (patch: Partial<GeneralConfig>) =>
    setCfg(c => c ? { ...c, general: { ...(c.general ?? {}), ...patch } } : c)

  const updateLogging = (patch: Partial<LoggingConfig>) =>
    setCfg(c => {
      if (!c) return c
      const logging = { ...(c.logging ?? {}), ...patch }
      return {
        ...c,
        logging,
        verbose_logging: logging.enabled ?? c.verbose_logging,
      }
    })

  const updateProvider = (i: number, patch: Partial<ProviderConfig>) =>
    setCfg(c => c ? { ...c, providers: c.providers.map((p, j) => j === i ? { ...p, ...patch } : p) } : c)
  const addProvider = () => {
    setCfg(c => c ? { ...c, providers: [...c.providers, EMPTY_PROVIDER()] } : c)
    setTimeout(() => setCfg(c => { if (c) setSelProvider(c.providers.length - 1); return c }), 0)
  }
  const removeProvider = (i: number) => {
    setCfg(c => c ? { ...c, providers: c.providers.filter((_, j) => j !== i) } : c)
    setSelProvider(p => Math.max(0, p > i ? p - 1 : p === i ? Math.max(0, p - 1) : p))
  }

  const updateCombo = (i: number, patch: Partial<ComboConfig>) =>
    setCfg(c => c ? { ...c, combos: c.combos.map((cb, j) => j === i ? { ...cb, ...patch } : cb) } : c)

  const addCombo = (ownedBy?: string) => {
    const group = ownedBy || 'default'
    const isDef = group === 'default' || !group
    const newCb: ComboConfig = {
      ...EMPTY_COMBO(),
      owned_by: group,
      is_default: isDef,
    }
    setCfg(c => c ? { ...c, combos: [...c.combos, newCb] } : c)
    setTimeout(() => setCfg(c => { if (c) setSelCombo(c.combos.length - 1); return c }), 0)
  }

  const addComboGroup = () => {
    const groupName = window.prompt('请输入新分组名称（如 team-a, pro 等）:')
    if (!groupName) return
    const cleanName = groupName.trim().replace(/\//g, '-')
    if (!cleanName) return
    addCombo(cleanName)
  }

  const removeCombo = (i: number) => {
    setCfg(c => c ? { ...c, combos: c.combos.filter((_, j) => j !== i) } : c)
    setSelCombo(p => Math.max(0, p > i ? p - 1 : p === i ? Math.max(0, p - 1) : p))
  }

  const toggleGroupCollapse = (groupName: string) => {
    setCollapsedGroups(prev => {
      const next = new Set(prev)
      if (next.has(groupName)) next.delete(groupName)
      else next.add(groupName)
      return next
    })
  }

  const handleComboReorder = (fromIndex: number, toIndex: number, targetOwnedBy?: string) => {
    if (fromIndex === toIndex && !targetOwnedBy) return
    setCfg(prev => {
      if (!prev) return prev
      const newCombos = [...prev.combos]
      const [moved] = newCombos.splice(fromIndex, 1)
      if (targetOwnedBy !== undefined) {
        moved.owned_by = targetOwnedBy
        moved.is_default = targetOwnedBy === 'default' || !targetOwnedBy
      }
      newCombos.splice(toIndex, 0, moved)
      return { ...prev, combos: newCombos }
    })
    setSelCombo(toIndex)
  }

  const payloadScripts = (cfg?.payload_scripts ?? [])
  const updatePayloadScript = (i: number, patch: Partial<PayloadScript>) =>
    setCfg(c => c ? { ...c, payload_scripts: (c.payload_scripts ?? []).map((s, j) => j === i ? { ...s, ...patch } : s) } : c)
  const addPayloadScript = () => {
    const newIdx = payloadScripts.length
    setCfg(c => c ? { ...c, payload_scripts: [...(c.payload_scripts ?? []), EMPTY_PAYLOAD_SCRIPT()] } : c)
    setSelPayload(newIdx)
  }
  const removePayloadScript = (i: number) => {
    setCfg(c => c ? { ...c, payload_scripts: (c.payload_scripts ?? []).filter((_, j) => j !== i) } : c)
    setSelPayload(p => Math.max(0, p > i ? p - 1 : p === i ? Math.max(0, p - 1) : p))
  }
  const movePayloadScript = (i: number, dir: -1 | 1) => {
    const j = i + dir
    setCfg(c => {
      if (!c) return c
      const scripts = [...(c.payload_scripts ?? [])]
      if (j < 0 || j >= scripts.length) return c
      ;[scripts[i], scripts[j]] = [scripts[j], scripts[i]]
      return { ...c, payload_scripts: scripts }
    })
    setSelPayload(j)
  }

  const providerNames = cfg.providers.map(p => p.name).filter(Boolean)
  const curProvider = cfg.providers[selProvider]
  const curCombo = cfg.combos[selCombo]
  const curPayloadScript = payloadScripts[selPayload]

  return (
    <div className="page" style={{ paddingBottom: 80, display: 'flex', flexDirection: 'column', height: '100%' }}>

      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 20, paddingBottom: 16, borderBottom: '1px solid var(--border)' }}>
        <span style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.01em' }}>配置编辑</span>
        <span style={{ fontSize: 13, color: 'var(--text-2)', marginRight: 'auto' }}>修改后点击「保存」即时生效，无需重启</span>
        <button className="btn-primary" onClick={save} disabled={saving} style={{ minWidth: 120 }}>
          {saving ? '保存中…' : '保存并热重载'}
        </button>
      </div>

      {msg && <div className="alert ok" style={{ marginBottom: 12 }}>✓ {msg}</div>}
      {err && <div className="alert err" style={{ marginBottom: 12 }}>{err}</div>}

      {/* Tab bar */}
      <div style={{ display: 'flex', gap: 2, marginBottom: 16, background: 'var(--bg)', padding: 4, borderRadius: 8, border: '1px solid var(--border)', width: 'fit-content' }}>
        {(['general', 'providers', 'combos', 'payload'] as const).map(t => {
          const count = t === 'providers' ? cfg.providers.length : t === 'combos' ? cfg.combos.length : t === 'payload' ? payloadScripts.length : null
          const active = tab === t
          const label = t === 'general' ? '通用' : t === 'providers' ? 'Providers' : t === 'combos' ? 'Combos' : 'Payload 脚本'
          return (
            <button key={t} onClick={() => setTab(t)} style={{
              padding: '6px 16px',
              borderRadius: 6,
              border: 'none',
              background: active ? 'var(--bg-panel)' : 'transparent',
              color: active ? 'var(--text)' : 'var(--text-2)',
              fontWeight: active ? 600 : 400,
              fontSize: 13,
              boxShadow: active ? '0 1px 3px rgba(0,0,0,0.1)' : 'none',
              cursor: 'pointer',
              transition: 'all 0.12s',
              display: 'flex', alignItems: 'center', gap: 6,
            }}>
              {label}
              {count !== null && (
                <span style={{
                  fontSize: 11, padding: '1px 6px', borderRadius: 10,
                  background: active ? 'var(--accent-light)' : 'var(--bg-code)',
                  color: active ? 'var(--accent)' : 'var(--text-3)',
                  fontWeight: 500,
                }}>{count}</span>
              )}
            </button>
          )
        })}
      </div>

      {/* Master-detail layout */}
      <div style={{ display: 'flex', gap: 0, flex: 1, background: 'var(--bg-panel)', border: '1px solid var(--border)', borderRadius: 10, overflow: 'hidden', boxShadow: '0 1px 3px rgba(0,0,0,0.08)' }}>

        {/* 通用 tab: full-width panel, no sidebar */}
        {tab === 'general' && (
          <div style={{ flex: 1, overflowY: 'auto', padding: '24px 32px' }}>
            <GeneralPanel
              general={cfg.general ?? {}}
              logging={cfg.logging ?? { enabled: cfg.verbose_logging }}
              onUpdate={updateGeneral}
              onUpdateLogging={updateLogging}
            />
          </div>
        )}

        {/* ── List panel (providers / combos / payload) ── */}
        {tab !== 'general' && (<>
        {sideListCollapsed ? (
          <div style={{
            width: 36, minWidth: 36, borderRight: '1px solid var(--border)',
            display: 'flex', flexDirection: 'column', alignItems: 'center',
            background: 'var(--bg)', padding: '10px 0', gap: 14,
          }}>
            <button
              className="btn-icon"
              onClick={() => setSideListCollapsed(false)}
              title="展开列表"
              style={{ width: 26, height: 26, padding: 0 }}
            >
              <IconChevronRight />
            </button>
            <div style={{
              writingMode: 'vertical-rl', fontSize: 11, fontWeight: 600,
              letterSpacing: '0.08em', textTransform: 'uppercase',
              color: 'var(--text-3)', userSelect: 'none', cursor: 'pointer',
            }} onClick={() => setSideListCollapsed(false)}>
              {tab === 'providers' ? 'Providers' : tab === 'combos' ? 'Combos' : 'Payload 脚本'}
            </div>
          </div>
        ) : (
          <div style={{ width: 240, minWidth: 240, borderRight: '1px solid var(--border)', display: 'flex', flexDirection: 'column', background: 'var(--bg)' }}>
            <div style={{ padding: '10px 10px 8px 12px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 6 }}>
              <span style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.07em', color: 'var(--text-3)', flex: 1 }}>
                {tab === 'providers' ? 'Providers' : tab === 'combos' ? 'Combos 分组' : 'Payload 脚本'}
              </span>
              {tab === 'combos' ? (
                <div style={{ display: 'flex', gap: 3 }}>
                  <button className="btn-ghost" style={{ padding: '2px 5px', fontSize: 11 }}
                    onClick={addComboGroup} title="新建分组">
                    <IconFolder /> 分组
                  </button>
                  <button className="btn-ghost" style={{ padding: '2px 5px', fontSize: 11 }}
                    onClick={() => addCombo()} title="添加 Combo">
                    <IconPlus /> Combo
                  </button>
                </div>
              ) : (
                <button className="btn-ghost" style={{ padding: '2px 6px', fontSize: 11 }}
                  onClick={tab === 'providers' ? addProvider : addPayloadScript}>
                  <IconPlus /> 添加
                </button>
              )}
              <button
                className="btn-icon"
                onClick={() => setSideListCollapsed(true)}
                title="收起列表"
                style={{ width: 24, height: 24, padding: 0 }}
              >
                <IconChevronLeft />
              </button>
            </div>

            <div style={{ flex: 1, overflowY: 'auto' }}>
              {/* ── Providers List ── */}
              {tab === 'providers' && cfg.providers.map((p, idx) => {
                const selected = selProvider === idx
                const fmts = p.api.map(e => e.api_format)
                const label = p.name || '未命名 Provider'

                return (
                  <div key={idx}
                    onClick={() => setSelProvider(idx)}
                    style={{
                      padding: '9px 12px',
                      cursor: 'pointer',
                      borderBottom: '1px solid var(--border)',
                      background: selected ? 'var(--bg-panel)' : 'transparent',
                      borderLeft: selected ? '3px solid var(--accent)' : '3px solid transparent',
                      transition: 'background 0.1s',
                    }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
                      <span style={{
                        fontSize: 13, fontWeight: selected ? 600 : 400,
                        color: p.name ? 'var(--text)' : 'var(--text-3)',
                        flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                      }}>{label}</span>
                      {selected && <IconChevronRight />}
                    </div>
                    <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                      {fmts.slice(0, 3).map(f => (
                        <span key={f} className={`tag ${FMT_COLOR[f as ApiFormat]}`} style={{ fontSize: 10, padding: '1px 5px' }}>
                          {f.replace('openai-', '').replace('openai', 'oai')}
                        </span>
                      ))}
                      <span className="tag" style={{ fontSize: 10, padding: '1px 5px' }}>
                        {p.keys.length}k
                      </span>
                    </div>
                  </div>
                )
              })}

              {/* ── Combos Grouped List ── */}
              {tab === 'combos' && (
                <div style={{ display: 'flex', flexDirection: 'column', gap: 6, padding: '8px 0' }}>
                  {groupedCombos.length === 0 && (
                    <div style={{ padding: '24px 16px', textAlign: 'center', color: 'var(--text-3)', fontSize: 12 }}>
                      暂无 Combo，点击上方添加
                    </div>
                  )}
                  {groupedCombos.map(group => {
                    const isGroupCollapsed = collapsedGroups.has(group.ownedBy)
                    return (
                      <div key={group.ownedBy} style={{ borderBottom: '1px solid var(--border)', paddingBottom: 4 }}>
                        {/* Group Header */}
                        <div style={{
                          display: 'flex', alignItems: 'center', gap: 5,
                          padding: '5px 8px', background: 'var(--bg-hover)',
                          margin: '0 6px 4px', borderRadius: 5,
                          border: '1px solid var(--border)',
                        }}>
                          <button
                            type="button"
                            className="btn-icon"
                            onClick={() => toggleGroupCollapse(group.ownedBy)}
                            title={isGroupCollapsed ? '展开分组' : '折叠分组'}
                            style={{ width: 18, height: 18, padding: 0 }}
                          >
                            {isGroupCollapsed ? <IconChevronRight /> : <IconChevronDown />}
                          </button>
                          <span style={{
                            fontSize: 12, fontWeight: 600, color: 'var(--text)',
                            overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1,
                          }}>
                            {group.ownedBy}
                          </span>
                          {group.isDefault && (
                            <span className="tag" style={{ fontSize: 9, padding: '1px 4px', color: 'var(--text-3)', background: 'var(--bg)' }}>
                              默认
                            </span>
                          )}
                          <span style={{ fontSize: 10, color: 'var(--text-3)', fontWeight: 500 }}>
                            {group.combos.length}
                          </span>
                          <button
                            type="button"
                            className="btn-icon"
                            onClick={() => addCombo(group.ownedBy)}
                            title={`添加 Combo 到 ${group.ownedBy}`}
                            style={{ width: 18, height: 18, padding: 0 }}
                          >
                            <IconPlus />
                          </button>
                        </div>

                        {/* Group items */}
                        {!isGroupCollapsed && (
                          <div style={{ display: 'flex', flexDirection: 'column' }}>
                            {group.combos.map(({ combo: cb, globalIndex: idx }) => {
                              const selected = selCombo === idx
                              const fmts = normalizeFormats(cb.api_format)
                              const isDragging = draggedComboIndex === idx
                              const isDragOver = dragOverComboIndex === idx

                              return (
                                <div
                                  key={idx}
                                  draggable
                                  onDragStart={e => {
                                    e.dataTransfer.setData('text/plain', String(idx))
                                    setDraggedComboIndex(idx)
                                  }}
                                  onDragOver={e => {
                                    e.preventDefault()
                                    if (dragOverComboIndex !== idx) setDragOverComboIndex(idx)
                                  }}
                                  onDragLeave={() => {
                                    if (dragOverComboIndex === idx) setDragOverComboIndex(null)
                                  }}
                                  onDrop={e => {
                                    e.preventDefault()
                                    if (draggedComboIndex !== null && draggedComboIndex !== idx) {
                                      handleComboReorder(draggedComboIndex, idx, group.ownedBy)
                                    }
                                    setDraggedComboIndex(null)
                                    setDragOverComboIndex(null)
                                  }}
                                  onDragEnd={() => {
                                    setDraggedComboIndex(null)
                                    setDragOverComboIndex(null)
                                  }}
                                  onClick={() => setSelCombo(idx)}
                                  style={{
                                    padding: '7px 8px 7px 6px',
                                    cursor: 'grab',
                                    borderBottom: '1px solid var(--border)',
                                    borderTop: isDragOver ? '2px solid var(--accent)' : 'none',
                                    background: selected ? 'var(--bg-panel)' : isDragging ? 'var(--bg-hover)' : 'transparent',
                                    borderLeft: selected ? '3px solid var(--accent)' : '3px solid transparent',
                                    opacity: isDragging ? 0.35 : 1,
                                    transition: 'background 0.1s',
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 5,
                                  }}
                                >
                                  <div
                                    style={{ color: 'var(--text-3)', cursor: 'grab', display: 'flex', alignItems: 'center', flexShrink: 0 }}
                                    title="按住拖拽排序 / 拖拽换组"
                                  >
                                    <IconGrip />
                                  </div>
                                  <div style={{ flex: 1, minWidth: 0 }}>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginBottom: 2 }}>
                                      <span style={{
                                        fontSize: 12.5, fontWeight: selected ? 600 : 400,
                                        color: cb.name ? 'var(--text)' : 'var(--text-3)',
                                        overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1,
                                      }}>
                                        {cb.name || '未命名 Combo'}
                                      </span>
                                      {selected && <IconChevronRight />}
                                    </div>
                                    <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                                      {fmts.slice(0, 2).map(f => (
                                        <span key={f} className={`tag ${FMT_COLOR[f as ApiFormat]}`} style={{ fontSize: 9, padding: '1px 4px' }}>
                                          {f.replace('openai-', '').replace('openai', 'oai')}
                                        </span>
                                      ))}
                                      <span className="tag" style={{ fontSize: 9, padding: '1px 4px' }}>
                                        {cb.members.length}m
                                      </span>
                                      {(cb.aliases ?? []).length > 0 && (
                                        <span className="tag" style={{ fontSize: 9, padding: '1px 4px', color: 'var(--text-2)' }}>
                                          +{(cb.aliases ?? []).length}
                                        </span>
                                      )}
                                    </div>
                                  </div>
                                </div>
                              )
                            })}
                          </div>
                        )}
                      </div>
                    )
                  })}
                </div>
              )}

              {/* ── Payload Scripts List ── */}
              {tab === 'payload' && payloadScripts.map((ps, idx) => {
                const selected = selPayload === idx
                return (
                  <div key={idx}
                    onClick={() => setSelPayload(idx)}
                    style={{
                      padding: '9px 12px',
                      cursor: 'pointer',
                      borderBottom: '1px solid var(--border)',
                      background: selected ? 'var(--bg-panel)' : 'transparent',
                      borderLeft: selected ? '3px solid var(--accent)' : '3px solid transparent',
                      transition: 'background 0.1s',
                      opacity: ps.enabled ? 1 : 0.55,
                    }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      <span style={{
                        fontSize: 13, fontWeight: selected ? 500 : 400,
                        color: ps.name ? 'var(--text)' : 'var(--text-3)',
                        flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                      }}>{ps.name || '未命名脚本'}</span>
                      {!ps.enabled && (
                        <span className="tag" style={{ fontSize: 10, padding: '1px 5px', color: 'var(--text-3)' }}>已禁用</span>
                      )}
                      {selected && <IconChevronRight />}
                    </div>
                  </div>
                )
              })}
            </div>
          </div>
        )}

        {/* ── Detail panel ── */}
        <div style={{ flex: 1, overflowY: 'auto', padding: '20px 28px' }}>
          {tab === 'providers' && (
            curProvider ? (
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 24, paddingBottom: 16, borderBottom: '1px solid var(--border)' }}>
                  <span style={{ fontSize: 16, fontWeight: 600 }}>
                    {curProvider.name || <span style={{ color: 'var(--text-3)', fontStyle: 'italic' }}>未命名 Provider</span>}
                  </span>
                  <div style={{ marginLeft: 'auto' }}>
                    <button className="btn-icon" title="删除此 Provider"
                      onClick={() => removeProvider(selProvider)}
                      style={{ width: 'auto', padding: '4px 10px', fontSize: 12, display: 'flex', alignItems: 'center', gap: 4, color: 'var(--err-fg)', border: '1px solid var(--border)' }}>
                      <IconTrash /> 删除
                    </button>
                  </div>
                </div>
                <ProviderDetail
                  p={curProvider}
                  onUpdate={patch => updateProvider(selProvider, patch)}
                />
              </div>
            ) : (
              <div className="empty-state" style={{ paddingTop: 80 }}>
                <div style={{ marginBottom: 12 }}>还没有 Provider</div>
                <button className="btn-primary" onClick={addProvider}><IconPlus /> 添加第一个 Provider</button>
              </div>
            )
          )}

          {tab === 'combos' && (
            curCombo ? (
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 24, paddingBottom: 16, borderBottom: '1px solid var(--border)' }}>
                  <span style={{ fontSize: 16, fontWeight: 600 }}>
                    {curCombo.name || <span style={{ color: 'var(--text-3)', fontStyle: 'italic' }}>未命名 Combo</span>}
                  </span>
                  <span className="tag" style={{ fontSize: 11, color: 'var(--accent)', background: 'var(--accent-light)' }}>
                    分组: {curCombo.owned_by || 'default'}
                    {(curCombo.is_default || curCombo.owned_by === 'default' || !curCombo.owned_by) ? ' (默认)' : ''}
                  </span>
                  <div style={{ marginLeft: 'auto' }}>
                    <button className="btn-icon" title="删除此 Combo"
                      onClick={() => removeCombo(selCombo)}
                      style={{ width: 'auto', padding: '4px 10px', fontSize: 12, display: 'flex', alignItems: 'center', gap: 4, color: 'var(--err-fg)', border: '1px solid var(--border)' }}>
                      <IconTrash /> 删除
                    </button>
                  </div>
                </div>
                <ComboDetail
                  cb={curCombo}
                  providerNames={providerNames}
                  existingGroups={existingGroups}
                  onUpdate={patch => updateCombo(selCombo, patch)}
                />
              </div>
            ) : (
              <div className="empty-state" style={{ paddingTop: 80 }}>
                <div style={{ marginBottom: 12 }}>还没有 Combo</div>
                <button className="btn-primary" onClick={() => addCombo()}><IconPlus /> 添加第一个 Combo</button>
              </div>
            )
          )}

          {tab === 'payload' && (
            curPayloadScript ? (
              <div>
                {/* Header: name + enabled toggle + sort + delete */}
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 20, paddingBottom: 16, borderBottom: '1px solid var(--border)' }}>
                  <input
                    value={curPayloadScript.name}
                    onChange={e => updatePayloadScript(selPayload, { name: e.target.value })}
                    placeholder="脚本名称"
                    style={{ flex: 1, fontSize: 15, fontWeight: 500 }}
                  />
                  {/* Enabled toggle */}
                  <label style={{
                    display: 'flex', alignItems: 'center', gap: 6, cursor: 'pointer',
                    padding: '5px 10px', borderRadius: 5,
                    border: `1px solid ${curPayloadScript.enabled ? 'var(--accent)' : 'var(--border-md)'}`,
                    background: curPayloadScript.enabled ? 'var(--accent-light)' : 'var(--bg-input)',
                    fontSize: 12, fontWeight: 500, userSelect: 'none',
                  }}>
                    <input type="checkbox" checked={curPayloadScript.enabled}
                      onChange={e => updatePayloadScript(selPayload, { enabled: e.target.checked })}
                    />
                    {curPayloadScript.enabled ? '已启用' : '已禁用'}
                  </label>
                  {/* Sort buttons */}
                  <button className="btn-icon" title="上移" disabled={selPayload === 0}
                    onClick={() => movePayloadScript(selPayload, -1)}
                    style={{ padding: '4px 8px' }}>
                    ↑
                  </button>
                  <button className="btn-icon" title="下移" disabled={selPayload >= payloadScripts.length - 1}
                    onClick={() => movePayloadScript(selPayload, 1)}
                    style={{ padding: '4px 8px' }}>
                    ↓
                  </button>
                  {/* Delete */}
                  <button className="btn-icon" onClick={() => removePayloadScript(selPayload)}
                    style={{ width: 'auto', padding: '4px 10px', fontSize: 12, display: 'flex', alignItems: 'center', gap: 4, color: 'var(--err-fg)', border: '1px solid var(--border)' }}>
                    <IconTrash /> 删除
                  </button>
                </div>

                {/* Script env reference */}
                <div style={{
                  display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 6,
                  background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: 6,
                  padding: '8px 12px', marginBottom: 12, fontSize: 12, color: 'var(--text-2)',
                }}>
                  {[
                    { name: 'body', desc: '请求 body (map)，可用 set/del/setpath 改写' },
                    { name: 'headers', desc: '请求 headers (map)，可用 seth/delh 改写' },
                    { name: 'combo', desc: '客户端传的 combo 名（只读）' },
                    { name: 'path', desc: '请求路径，如 /v1/chat/completions（只读）' },
                    { name: 'provider', desc: '选中的上游 provider 名（只读）' },
                    { name: 'model', desc: '发给上游的模型 ID（只读）' },
                    { name: 'api_format', desc: '上游 API 格式（只读）' },
                  ].map(v => (
                    <div key={v.name}>
                      <code style={{ fontFamily: 'var(--font-mono)', color: 'var(--accent)' }}>{v.name}</code>
                      <br /><span style={{ color: 'var(--text-3)' }}>{v.desc}</span>
                    </div>
                  ))}
                </div>
                <div style={{
                  background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: 6,
                  padding: '6px 12px', marginBottom: 12, fontSize: 11, color: 'var(--text-3)',
                  fontFamily: 'var(--font-mono)',
                }}>
                  {`内置函数：set(body,"k",v)  del(body,"k")  setpath(body,"k1","k2",v)  get(body,"k",def)  clamp(v,min,max)  seth(headers,"k",v)  delh(headers,"k")`}
                </div>

                {/* Script editor */}
                <textarea
                  value={curPayloadScript.script}
                  onChange={e => updatePayloadScript(selPayload, { script: e.target.value })}
                  spellCheck={false}
                  rows={20}
                  placeholder={`# 示例：隐藏 user-agent
delh(headers, "user-agent")

# 示例：改写 thinking 类型（三元表达式，可多行）
combo == "deepseek-v4-flash" && "thinking" in body && body.thinking.type == "adaptive"
  ? setpath(body, "thinking", "type", "enabled")
  : nil`}
                  style={{
                    width: '100%',
                    fontFamily: 'var(--font-mono)',
                    fontSize: 13,
                    lineHeight: 1.6,
                    padding: '12px 14px',
                    background: 'var(--bg)',
                    border: '1px solid var(--border-md)',
                    borderRadius: 6,
                    color: 'var(--text)',
                    resize: 'vertical',
                    boxSizing: 'border-box',
                    outline: 'none',
                    tabSize: 2,
                  }}
                />
                <div style={{ marginTop: 8, fontSize: 12, color: 'var(--text-3)' }}>
                  脚本语法：<a href="https://expr-lang.org" target="_blank" rel="noreferrer" style={{ color: 'var(--accent)' }}>expr-lang</a>，每行一条独立表达式，以空行分隔；多行三元表达式可连续书写。
                  脚本异常时请求原样转发，异常摘要记录在请求明细 <code style={{ fontFamily: 'var(--font-mono)', fontSize: 11 }}>matched_payload</code> 字段。
                </div>
              </div>
            ) : (
              <div className="empty-state" style={{ paddingTop: 80 }}>
                <div style={{ marginBottom: 12 }}>还没有 Payload 脚本</div>
                <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 16, maxWidth: 400 }}>
                  脚本按顺序执行，通过 <code style={{ fontFamily: 'var(--font-mono)' }}>body</code> / <code style={{ fontFamily: 'var(--font-mono)' }}>headers</code> 变量改写请求，
                  支持 <code style={{ fontFamily: 'var(--font-mono)' }}>combo</code>、<code style={{ fontFamily: 'var(--font-mono)' }}>provider</code>、<code style={{ fontFamily: 'var(--font-mono)' }}>model</code>、<code style={{ fontFamily: 'var(--font-mono)' }}>api_format</code> 等上下文变量。
                  可用于隐藏客户端标识、按 combo 调整 thinking 参数等。
                </div>
                <button className="btn-primary" onClick={addPayloadScript}><IconPlus /> 添加第一个脚本</button>
              </div>
            )
          )}
        </div>
        </>)} {/* end tab !== 'general' */}
      </div>
    </div>
  )
}
