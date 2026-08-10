import { useEffect, useState } from 'react'
import { getConfig, putConfig, FMT_ENDPOINT, FMT_COLOR, normalizeFormats } from '../api/client'
import type {
  AppConfig, ProviderConfig, ComboConfig, HealthCheckRule, ComboMember, ApiEndpoint, ApiFormat,
  PayloadScript, GeneralConfig, ProxyConfig,
} from '../api/client'

// Client-facing formats (proxy routes exist for these).
const CLIENT_FORMATS: ApiFormat[] = ['openai', 'anthropic', 'openai-responses', 'openai-images']
// All formats including upstream-only (for upstream_api_format hint).
const ALL_FORMATS: ApiFormat[] = ['openai', 'anthropic', 'openai-responses', 'openai-images', 'gemini']

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
  name: '', api_format: ['openai'], strategy: 'fill-first',
  members: [{ provider: '', model: '' }], aliases: [],
})
const EMPTY_PAYLOAD_SCRIPT = (): PayloadScript => ({ name: '', enabled: true, script: '' })


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
function ComboDetail({
  cb, providerNames, onUpdate,
}: {
  cb: ComboConfig
  providerNames: string[]
  onUpdate: (patch: Partial<ComboConfig>) => void
}) {
  const formats = normalizeFormats(cb.api_format)
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 28 }}>
      {/* Basic */}
      <div>
        <SectionLabel>基本设置</SectionLabel>
        <FieldRow label="名称" hint="客户端 model 字段填此值">
          <input value={cb.name} placeholder="fast" onChange={e => onUpdate({ name: e.target.value })} />
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
            display: 'grid', gridTemplateColumns: '24px 1fr 1fr 180px 32px',
            gap: 8, padding: '0 10px', marginBottom: 2,
          }}>
            {['#', 'Provider', '上游模型 ID', '上游 API 格式（可选）', ''].map((h, i) => (
              <span key={i} style={{ fontSize: 11, color: 'var(--text-3)' }}>{h}</span>
            ))}
          </div>
          {cb.members.map((m, mi) => (
            <div key={mi} style={{
              display: 'grid', gridTemplateColumns: '24px 1fr 1fr 180px 32px',
              gap: 8, alignItems: 'center',
              padding: '8px 10px',
              background: 'var(--bg)',
              border: '1px solid var(--border)',
              borderRadius: 'var(--radius)',
            }}>
              <span style={{ fontSize: 11, color: 'var(--text-3)', fontFamily: 'var(--font-mono)', textAlign: 'center' }}>{mi + 1}</span>
              <select value={m.provider}
                onChange={e => onUpdate({
                  members: cb.members.map((mm, mj): ComboMember => mj === mi ? { ...mm, provider: e.target.value } : mm),
                })}>
                <option value="">— Provider —</option>
                {providerNames.map(n => <option key={n} value={n}>{n}</option>)}
              </select>
              <input value={m.model} placeholder="上游模型 ID"
                style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
                onChange={e => onUpdate({
                  members: cb.members.map((mm, mj): ComboMember => mj === mi ? { ...mm, model: e.target.value } : mm),
                })} />
              {/* upstream_api_format: empty = same as combo's client format (no translation) */}
              <select
                value={m.upstream_api_format ?? ''}
                title="留空表示与客户端格式相同（无需翻译）"
                onChange={e => onUpdate({
                  members: cb.members.map((mm, mj): ComboMember =>
                    mj === mi ? { ...mm, upstream_api_format: e.target.value || undefined } : mm
                  ),
                })}
                style={{ fontSize: 12, color: m.upstream_api_format ? 'var(--text)' : 'var(--text-3)' }}
              >
                <option value="">— 同客户端格式 —</option>
                {ALL_FORMATS.map(f => <option key={f} value={f}>{f}</option>)}
              </select>
              <button className="btn-icon" disabled={cb.members.length <= 1}
                onClick={() => onUpdate({ members: cb.members.filter((_, mj) => mj !== mi) })}>
                <IconTrash />
              </button>
            </div>
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
  onUpdate,
}: {
  general: GeneralConfig
  onUpdate: (patch: Partial<GeneralConfig>) => void
}) {
  const [revealedKeys, setRevealedKeys] = useState<Set<number>>(new Set())
  const toggleReveal = (ki: number) =>
    setRevealedKeys(prev => {
      const next = new Set(prev)
      next.has(ki) ? next.delete(ki) : next.add(ki)
      return next
    })

  const apiKeys = general.api_keys ?? []
  const proxy = general.proxy ?? {}

  const updateProxy = (patch: Partial<ProxyConfig>) =>
    onUpdate({ proxy: { ...proxy, ...patch } })

  const proxyEnabled = !!(proxy.url || proxy.disabled !== undefined)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 32, maxWidth: 720 }}>

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

  useEffect(() => {
    getConfig()
      .then(raw => setCfg({
        ...raw,
        general: {
          ...EMPTY_GENERAL(),
          ...raw.general,
          api_keys: raw.general?.api_keys ?? [],
        },
        combos: raw.combos.map(c => ({
          aliases: [],
          ...c,
          api_format: normalizeFormats(c.api_format),
          members: c.members.map(m => ({ ...m, upstream_api_format: m.upstream_api_format ?? '' })),
        })),
        payload_scripts: raw.payload_scripts ?? [],
      }))
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

      await putConfig({
        ...cfg,
        general: cleanGeneral,
        combos: cfg.combos.map(c => ({
          ...c,
          api_format: (c.api_format as ApiFormat[]).length === 1
            ? (c.api_format as ApiFormat[])[0]
            : c.api_format,
          members: c.members.map(m => {
            const mm = { ...m }
            if (!mm.upstream_api_format) delete mm.upstream_api_format
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
  const addCombo = () => {
    setCfg(c => c ? { ...c, combos: [...c.combos, EMPTY_COMBO()] } : c)
    setTimeout(() => setCfg(c => { if (c) setSelCombo(c.combos.length - 1); return c }), 0)
  }
  const removeCombo = (i: number) => {
    setCfg(c => c ? { ...c, combos: c.combos.filter((_, j) => j !== i) } : c)
    setSelCombo(p => Math.max(0, p > i ? p - 1 : p === i ? Math.max(0, p - 1) : p))
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
              onUpdate={updateGeneral}
            />
          </div>
        )}

        {/* ── List panel (providers / combos / payload) ── */}
        {tab !== 'general' && (<>
        <div style={{ width: 220, minWidth: 220, borderRight: '1px solid var(--border)', display: 'flex', flexDirection: 'column', background: 'var(--bg)' }}>
          <div style={{ padding: '10px 12px 8px', borderBottom: '1px solid var(--border)', display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.07em', color: 'var(--text-3)', flex: 1 }}>
              {tab === 'providers' ? 'Providers' : tab === 'combos' ? 'Combos' : 'Payload 脚本'}
            </span>
            <button className="btn-ghost" style={{ padding: '2px 6px', fontSize: 11 }}
              onClick={tab === 'providers' ? addProvider : tab === 'combos' ? addCombo : addPayloadScript}>
              <IconPlus /> 添加
            </button>
          </div>
          <div style={{ flex: 1, overflowY: 'auto' }}>
            {tab !== 'payload' && (tab === 'providers' ? cfg.providers : cfg.combos).map((item, idx) => {
              const isProvider = tab === 'providers'
              const selected = isProvider ? selProvider === idx : selCombo === idx
              const p = item as ProviderConfig
              const cb = item as ComboConfig
              const fmts = isProvider
                ? p.api.map(e => e.api_format)
                : normalizeFormats(cb.api_format)
              const label = item.name || (isProvider ? '未命名 Provider' : '未命名 Combo')

              return (
                <div key={idx}
                  onClick={() => isProvider ? setSelProvider(idx) : setSelCombo(idx)}
                  style={{
                    padding: '9px 12px',
                    cursor: 'pointer',
                    borderBottom: '1px solid var(--border)',
                    background: selected ? 'var(--bg-panel)' : 'transparent',
                    borderLeft: selected ? '2px solid var(--accent)' : '2px solid transparent',
                    transition: 'background 0.1s',
                  }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 4 }}>
                    <span style={{
                      fontSize: 13, fontWeight: selected ? 500 : 400,
                      color: item.name ? 'var(--text)' : 'var(--text-3)',
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
                    {isProvider && (
                      <span className="tag" style={{ fontSize: 10, padding: '1px 5px' }}>
                        {p.keys.length}k
                      </span>
                    )}
                    {!isProvider && (
                      <span className="tag" style={{ fontSize: 10, padding: '1px 5px' }}>
                        {cb.members.length}m
                      </span>
                    )}
                    {!isProvider && (cb.aliases ?? []).length > 0 && (
                      <span className="tag" style={{ fontSize: 10, padding: '1px 5px', color: 'var(--text-2)' }}>
                        +{(cb.aliases ?? []).length}别名
                      </span>
                    )}
                  </div>
                </div>
              )
            })}
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
                    borderLeft: selected ? '2px solid var(--accent)' : '2px solid transparent',
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
                  onUpdate={patch => updateCombo(selCombo, patch)}
                />
              </div>
            ) : (
              <div className="empty-state" style={{ paddingTop: 80 }}>
                <div style={{ marginBottom: 12 }}>还没有 Combo</div>
                <button className="btn-primary" onClick={addCombo}><IconPlus /> 添加第一个 Combo</button>
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
