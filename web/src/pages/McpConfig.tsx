import { useEffect, useMemo, useState } from 'react'
import {
  getMcpConfig,
  putMcpConfig,
  testMcpProvider,
  getMcpProviderCapabilities,
  EMPTY_MCP_PROVIDER,
  EMPTY_MCP_COMBO,
} from '../api/client'
import type {
  McpProviderConfig,
  McpComboConfig,
  McpComboMemberConfig,
  McpTestResult,
  McpCapabilitiesResult,
  McpAuthConfig,
} from '../api/client'

// ── Icons ────────────────────────────────────────────────────────
function IconPlus() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2">
      <line x1="8" y1="2" x2="8" y2="14" /><line x1="2" y1="8" x2="14" y2="8" />
    </svg>
  )
}

function IconTrash() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8">
      <polyline points="3 4 13 4" />
      <path d="M5 4V3a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v1" />
      <path d="M6 7v5M10 7v5M4 4l1 9h6l1-9" />
    </svg>
  )
}

function IconZap() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8">
      <polygon points="9 1 3 9 8 9 7 15 13 7 8 7 9 1" />
    </svg>
  )
}

function IconCopy() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6">
      <rect x="5" y="5" width="9" height="9" rx="1.5" />
      <path d="M11 5V3a1.5 1.5 0 0 0-1.5-1.5H3A1.5 1.5 0 0 0 1.5 3v6.5A1.5 1.5 0 0 0 3 11h2" />
    </svg>
  )
}

// ── Form Components ──────────────────────────────────────────────
function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div style={{
      fontSize: 11, fontWeight: 700, textTransform: 'uppercase',
      letterSpacing: '0.08em', color: 'var(--text-3)',
      padding: '0 0 8px', marginBottom: 12,
      borderBottom: '1px solid var(--border)',
    }}>{children}</div>
  )
}

function FieldRow({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '160px 1fr', gap: '0 16px', alignItems: 'start', padding: '8px 0' }}>
      <div style={{ paddingTop: 6 }}>
        <div style={{ fontSize: 13, fontWeight: 500, color: 'var(--text)' }}>{label}</div>
        {hint && <div style={{ fontSize: 11, color: 'var(--text-3)', marginTop: 2, lineHeight: 1.4 }}>{hint}</div>}
      </div>
      <div>{children}</div>
    </div>
  )
}

// ── Provider Detail Form ─────────────────────────────────────────
function ProviderDetail({
  p,
  onUpdate,
  onTest,
  testState,
}: {
  p: McpProviderConfig
  onUpdate: (patch: Partial<McpProviderConfig>) => void
  onTest: () => void
  testState?: McpTestResult | 'testing'
}) {
  const auth = p.auth || { mode: 'none', isolated: false }
  const isIsolated = auth.isolated || p.auth_mode === 'isolated'

  const updateAuth = (patch: Partial<McpAuthConfig>) => {
    const updated = { ...auth, ...patch }
    onUpdate({
      auth: updated,
      auth_mode: updated.isolated ? 'isolated' : 'shared',
    })
  }

  // Key-value editor for Env (stdio)
  const envEntries = Object.entries(p.env || {})
  const updateEnvKey = (oldKey: string, newKey: string, val: string) => {
    const next = { ...(p.env || {}) }
    if (oldKey !== newKey) delete next[oldKey]
    if (newKey) next[newKey] = val
    onUpdate({ env: next })
  }
  const updateEnvVal = (key: string, val: string) => {
    const next = { ...(p.env || {}), [key]: val }
    onUpdate({ env: next })
  }
  const deleteEnv = (key: string) => {
    const next = { ...(p.env || {}) }
    delete next[key]
    onUpdate({ env: next })
  }

  // Key-value editor for Headers (sse / http)
  const headerEntries = Object.entries(p.headers || {})
  const updateHeaderKey = (oldKey: string, newKey: string, val: string) => {
    const next = { ...(p.headers || {}) }
    if (oldKey !== newKey) delete next[oldKey]
    if (newKey) next[newKey] = val
    onUpdate({ headers: next })
  }
  const updateHeaderVal = (key: string, val: string) => {
    const next = { ...(p.headers || {}), [key]: val }
    onUpdate({ headers: next })
  }
  const deleteHeader = (key: string) => {
    const next = { ...(p.headers || {}) }
    delete next[key]
    onUpdate({ headers: next })
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24, padding: '20px 24px' }}>
      {/* 1. Basic Settings */}
      <div>
        <SectionLabel>基本设置</SectionLabel>
        <FieldRow label="服务名称" hint="唯一标识，用于在 Combos 中被引用">
          <input
            value={p.name}
            placeholder="例如 fs-local, github-mcp"
            onChange={e => onUpdate({ name: e.target.value.trim() })}
            style={{ maxWidth: 320 }}
          />
        </FieldRow>
        <FieldRow label="传输协议" hint="MCP 规范定义的通信协议类型">
          <select
            value={p.transport}
            onChange={e => onUpdate({ transport: e.target.value as McpProviderConfig['transport'] })}
            style={{ maxWidth: 320 }}
          >
            <option value="stdio">stdio (本地子进程命令行)</option>
            <option value="streamablehttp">streamablehttp (Streamable HTTP 单端点)</option>
            <option value="sse">sse (Server-Sent Events 长连接)</option>
          </select>
        </FieldRow>
        <FieldRow label="请求超时" hint="与该上游交互的默认超时秒数">
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <input
              type="number"
              min={1}
              value={p.timeout_seconds || 30}
              onChange={e => onUpdate({ timeout_seconds: parseInt(e.target.value) || 30 })}
              style={{ width: 90 }}
            />
            <span style={{ fontSize: 12, color: 'var(--text-3)' }}>秒</span>
          </div>
        </FieldRow>
      </div>

      {/* 2. Transport-Specific Settings */}
      {p.transport === 'stdio' ? (
        <div>
          <SectionLabel>子进程命令行设置 (stdio)</SectionLabel>
          <FieldRow label="执行程序" hint="PATH 中的可执行命令或绝对路径">
            <input
              value={p.command || ''}
              placeholder="例如 npx, python3, node, uvx"
              onChange={e => onUpdate({ command: e.target.value })}
              style={{ maxWidth: 400, fontFamily: 'var(--font-mono)', fontSize: 13 }}
            />
          </FieldRow>
          <FieldRow label="命令行参数" hint="每项一个参数">
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {(p.args || []).map((arg, idx) => (
                <div key={idx} style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                  <input
                    value={arg}
                    onChange={e => {
                      const next = [...(p.args || [])]
                      next[idx] = e.target.value
                      onUpdate({ args: next })
                    }}
                    style={{ flex: 1, fontFamily: 'var(--font-mono)', fontSize: 12 }}
                  />
                  <button
                    className="btn-icon"
                    title="删除"
                    onClick={() => onUpdate({ args: (p.args || []).filter((_, i) => i !== idx) })}
                  >
                    <IconTrash />
                  </button>
                </div>
              ))}
              <button
                className="btn-add"
                onClick={() => onUpdate({ args: [...(p.args || []), ''] })}
                style={{ width: 'fit-content' }}
              >
                <IconPlus /> 添加参数
              </button>
            </div>
          </FieldRow>
          <FieldRow label="工作目录" hint="子进程执行的根路径（可选）">
            <input
              value={p.working_dir || ''}
              placeholder="例如 /path/to/repo"
              onChange={e => onUpdate({ working_dir: e.target.value })}
              style={{ maxWidth: 400, fontFamily: 'var(--font-mono)', fontSize: 12 }}
            />
          </FieldRow>
          <FieldRow label="环境变量" hint="注入到子进程的环境变量键值对">
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {envEntries.map(([k, v], idx) => (
                <div key={idx} style={{ display: 'grid', gridTemplateColumns: '160px 1fr 32px', gap: 8, alignItems: 'center' }}>
                  <input
                    value={k}
                    placeholder="KEY"
                    onChange={e => updateEnvKey(k, e.target.value, v)}
                    style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
                  />
                  <input
                    value={v}
                    placeholder="VALUE"
                    onChange={e => updateEnvVal(k, e.target.value)}
                    style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
                  />
                  <button className="btn-icon" onClick={() => deleteEnv(k)}>
                    <IconTrash />
                  </button>
                </div>
              ))}
              <button
                className="btn-add"
                onClick={() => updateEnvVal(`ENV_VAR_${envEntries.length + 1}`, '')}
                style={{ width: 'fit-content' }}
              >
                <IconPlus /> 添加环境变量
              </button>
            </div>
          </FieldRow>
        </div>
      ) : (
        <div>
          <SectionLabel>远程服务设置 ({p.transport})</SectionLabel>
          <FieldRow label="服务 URL" hint={p.transport === 'sse' ? 'MCP SSE 端点 URL' : 'MCP HTTP POST 端点 URL'}>
            <input
              value={p.url || ''}
              placeholder="https://example.com/mcp"
              onChange={e => onUpdate({ url: e.target.value })}
              style={{ maxWidth: 480, fontFamily: 'var(--font-mono)', fontSize: 13 }}
            />
          </FieldRow>
          <FieldRow label="固定 Headers" hint="每次向上游发请求时附加的静态头">
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {headerEntries.map(([k, v], idx) => (
                <div key={idx} style={{ display: 'grid', gridTemplateColumns: '160px 1fr 32px', gap: 8, alignItems: 'center' }}>
                  <input
                    value={k}
                    placeholder="Header-Name"
                    onChange={e => updateHeaderKey(k, e.target.value, v)}
                    style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
                  />
                  <input
                    value={v}
                    placeholder="Header-Value"
                    onChange={e => updateHeaderVal(k, e.target.value)}
                    style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}
                  />
                  <button className="btn-icon" onClick={() => deleteHeader(k)}>
                    <IconTrash />
                  </button>
                </div>
              ))}
              <button
                className="btn-add"
                onClick={() => updateHeaderVal(`X-Custom-${headerEntries.length + 1}`, '')}
                style={{ width: 'fit-content' }}
              >
                <IconPlus /> 添加 Header
              </button>
            </div>
          </FieldRow>
        </div>
      )}

      {/* 3. Authentication & Multi-user Isolation */}
      <div>
        <SectionLabel>认证与多用户隔离 (Authentication)</SectionLabel>
        <FieldRow label="多用户隔离" hint="是否为不同最终用户独立维护凭据">
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, cursor: 'pointer', fontSize: 13 }}>
            <input
              type="checkbox"
              checked={isIsolated}
              onChange={e => updateAuth({ isolated: e.target.checked })}
            />
            <span>开启用户隔离认证 (按下游 Header 传递的 User ID 独立认证)</span>
          </label>
        </FieldRow>
        <FieldRow label="认证方式" hint="与上游服务握手鉴权的机制">
          <select
            value={auth.mode || 'none'}
            onChange={e => updateAuth({ mode: e.target.value as any })}
            style={{ maxWidth: 280 }}
          >
            <option value="none">none — 无额外鉴权</option>
            <option value="api_key">api_key — 静态 Header 凭据</option>
            <option value="oauth2">oauth2 — OAuth 2.1 授权码 (PKCE 流程)</option>
          </select>
        </FieldRow>

        {auth.mode === 'api_key' && (
          <>
            <FieldRow label="Header Name" hint="默认为 Authorization">
              <input
                value={auth.header || auth.header_name || 'Authorization'}
                placeholder="Authorization"
                onChange={e => updateAuth({ header: e.target.value, header_name: e.target.value })}
                style={{ maxWidth: 240, fontFamily: 'var(--font-mono)' }}
              />
            </FieldRow>
            <FieldRow label="Header Value" hint="如 Bearer your-token">
              <input
                value={auth.api_key || auth.header_value || ''}
                placeholder="Bearer your-secret-token"
                onChange={e => updateAuth({ api_key: e.target.value, header_value: e.target.value })}
                style={{ maxWidth: 360, fontFamily: 'var(--font-mono)' }}
              />
            </FieldRow>
          </>
        )}

        {auth.mode === 'oauth2' && (
          <>
            <FieldRow label="Client ID">
              <input
                value={auth.client_id || ''}
                placeholder="OAuth 应用 Client ID"
                onChange={e => updateAuth({ client_id: e.target.value })}
                style={{ maxWidth: 360, fontFamily: 'var(--font-mono)' }}
              />
            </FieldRow>
            <FieldRow label="Client Secret">
              <input
                type="password"
                value={auth.client_secret || ''}
                placeholder="OAuth 应用 Client Secret"
                onChange={e => updateAuth({ client_secret: e.target.value })}
                style={{ maxWidth: 360, fontFamily: 'var(--font-mono)' }}
              />
            </FieldRow>
            <FieldRow label="授权地址 (Auth URL)" hint="OAuth 登录跳转地址">
              <input
                value={auth.auth_url || ''}
                placeholder="https://example.com/oauth/authorize"
                onChange={e => updateAuth({ auth_url: e.target.value })}
                style={{ maxWidth: 440, fontFamily: 'var(--font-mono)', fontSize: 12 }}
              />
            </FieldRow>
            <FieldRow label="令牌地址 (Token URL)" hint="换取 Access Token 的端点">
              <input
                value={auth.token_url || ''}
                placeholder="https://example.com/oauth/token"
                onChange={e => updateAuth({ token_url: e.target.value })}
                style={{ maxWidth: 440, fontFamily: 'var(--font-mono)', fontSize: 12 }}
              />
            </FieldRow>
            <FieldRow label="Scopes (权限)" hint="逗号或空格分隔">
              <input
                value={(auth.scopes || []).join(' ')}
                placeholder="read write repo"
                onChange={e => updateAuth({ scopes: e.target.value.split(/[,\s]+/).filter(Boolean) })}
                style={{ maxWidth: 360, fontFamily: 'var(--font-mono)' }}
              />
            </FieldRow>
          </>
        )}
      </div>

      {/* 4. Connectivity Test Card */}
      <div style={{
        marginTop: 8,
        padding: '16px 18px',
        background: 'var(--bg)',
        border: '1px solid var(--border)',
        borderRadius: 'var(--radius)',
      }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
          <div>
            <div style={{ fontWeight: 600, fontSize: 13, display: 'flex', alignItems: 'center', gap: 6 }}>
              <IconZap /> 上游连通性与工具探测
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-3)', marginTop: 2 }}>
              发送 <code>initialize</code> 握手并拉取可用工具列表 (需已保存至服务生效)
            </div>
          </div>
          <button
            className="btn"
            onClick={onTest}
            disabled={testState === 'testing' || !p.name}
            style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12 }}
          >
            {testState === 'testing' ? '正在探测...' : '测试连接'}
          </button>
        </div>

        {testState && testState !== 'testing' && (
          <div style={{ marginTop: 12, fontSize: 12 }}>
            {testState.success ? (
              <div>
                <div style={{ color: 'var(--ok-fg)', fontWeight: 600, marginBottom: 6 }}>
                  ✓ 连通正常 {testState.server_info && `(${testState.server_info.name} v${testState.server_info.version})`}
                </div>
                {testState.tools && testState.tools.length > 0 ? (
                  <div>
                    <div style={{ color: 'var(--text-2)', marginBottom: 6 }}>
                      探测到 {testState.tools.length} 个工具：
                    </div>
                    <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                      {testState.tools.map(t => (
                        <span key={t.name} className="badge badge-ok" style={{ fontFamily: 'var(--font-mono)' }}>
                          {t.name}
                        </span>
                      ))}
                    </div>
                  </div>
                ) : (
                  <div style={{ color: 'var(--text-3)' }}>该提供商未暴露任何工具。</div>
                )}
              </div>
            ) : (
              <div style={{ color: 'var(--err-fg)' }}>
                ✗ 探测失败：{testState.error}
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

// ── Combo Detail Form ────────────────────────────────────────────
function ComboDetail({
  c,
  allProviders,
  onUpdate,
}: {
  c: McpComboConfig
  allProviders: McpProviderConfig[]
  onUpdate: (patch: Partial<McpComboConfig>) => void
}) {
  const [providerCapsCache, setProviderCapsCache] = useState<Record<string, McpCapabilitiesResult>>({})
  const [loadingCaps, setLoadingCaps] = useState<Record<string, boolean>>({})
  const [activeCategory, setActiveCategory] = useState<Record<number, 'tools' | 'resources' | 'prompts'>>({})

  const loadCapsForProvider = async (provName: string, force = false) => {
    if (!provName) return
    if (!force && providerCapsCache[provName]) return
    setLoadingCaps(prev => ({ ...prev, [provName]: true }))
    try {
      const res = await getMcpProviderCapabilities(provName)
      setProviderCapsCache(prev => ({ ...prev, [provName]: res }))
    } catch (err: any) {
      setProviderCapsCache(prev => ({
        ...prev,
        [provName]: { success: false, error: err.message || String(err) },
      }))
    } finally {
      setLoadingCaps(prev => ({ ...prev, [provName]: false }))
    }
  }

  useEffect(() => {
    (c.members || []).forEach(m => {
      if (m.provider && !providerCapsCache[m.provider] && !loadingCaps[m.provider]) {
        loadCapsForProvider(m.provider)
      }
    })
  }, [c.members])

  const updateMember = (idx: number, patch: Partial<McpComboMemberConfig>) => {
    const next = (c.members || []).map((m, i) => (i === idx ? { ...m, ...patch } : m))
    onUpdate({ members: next })
  }

  const addMember = () => {
    const firstProv = allProviders[0]?.name || ''
    const next: McpComboMemberConfig = {
      provider: firstProv,
      prefix: firstProv ? firstProv.slice(0, 3) : '',
      tools: ['*'],
      resources: ['*'],
      prompts: ['*'],
    }
    onUpdate({ members: [...(c.members || []), next] })
    if (firstProv) loadCapsForProvider(firstProv)
  }

  const removeMember = (idx: number) => {
    onUpdate({ members: (c.members || []).filter((_, i) => i !== idx) })
  }

  const origin = window.location.origin

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24, padding: '20px 24px' }}>
      {/* 1. Basic Settings */}
      <div>
        <SectionLabel>基本设置</SectionLabel>
        <FieldRow label="网关名称" hint="下行暴露的 Combo 唯一名称（禁止包含 /）">
          <input
            value={c.name}
            placeholder="例如 all-tools, dev-box"
            onChange={e => onUpdate({ name: e.target.value.replace(/\//g, '-').trim() })}
            style={{ maxWidth: 320 }}
          />
        </FieldRow>
        <FieldRow label="说明描述" hint="将作为 instructions 返回给客户端">
          <input
            value={c.description || ''}
            placeholder="常用综合开发工具箱"
            onChange={e => onUpdate({ description: e.target.value })}
            style={{ maxWidth: 440 }}
          />
        </FieldRow>
        <FieldRow label="用户标识 Header" hint="聊天机器人透传最终用户 ID 的请求头">
          <input
            value={c.user_id_header || 'X-User-Id'}
            placeholder="X-User-Id"
            onChange={e => onUpdate({ user_id_header: e.target.value.trim() })}
            style={{ maxWidth: 240, fontFamily: 'var(--font-mono)' }}
          />
        </FieldRow>
      </div>

      {/* 2. Members */}
      <div>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
          <SectionLabel>包含成员提供商 (Members)</SectionLabel>
          <button className="btn-add" onClick={addMember} style={{ marginTop: -14 }}>
            <IconPlus /> 添加成员
          </button>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {(c.members || []).map((m, idx) => {
            const caps = providerCapsCache[m.provider]
            const isLoading = Boolean(loadingCaps[m.provider])
            const curCategory = activeCategory[idx] || 'tools'

            const toolsList = caps?.tools || []
            const resourcesList = caps?.resources || []
            const promptsList = caps?.prompts || []

            const memberTools = m.tools || []
            const isAllTools = memberTools.length === 1 && memberTools[0] === '*'

            const memberResources = m.resources || []
            const isAllResources = memberResources.length === 1 && memberResources[0] === '*'

            const memberPrompts = m.prompts || []
            const isAllPrompts = memberPrompts.length === 1 && memberPrompts[0] === '*'

            return (
              <div
                key={idx}
                style={{
                  background: 'var(--bg)',
                  border: '1px solid var(--border)',
                  borderRadius: 'var(--radius)',
                  padding: '14px 16px',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 14,
                }}
              >
                {/* Member Header Row */}
                <div style={{ display: 'grid', gridTemplateColumns: '220px 180px 1fr 32px', gap: 12, alignItems: 'center' }}>
                  <div>
                    <label style={{ fontSize: 11, color: 'var(--text-3)', display: 'block', marginBottom: 4 }}>
                      Provider 上游服务
                    </label>
                    <select
                      value={m.provider}
                      onChange={e => {
                        const newProv = e.target.value
                        updateMember(idx, { provider: newProv })
                        loadCapsForProvider(newProv)
                      }}
                    >
                      {allProviders.map(p => (
                        <option key={p.name} value={p.name}>
                          {p.name} ({p.transport})
                        </option>
                      ))}
                    </select>
                  </div>
                  <div>
                    <label style={{ fontSize: 11, color: 'var(--text-3)', display: 'block', marginBottom: 4 }}>
                      工具前缀 (Prefix)
                    </label>
                    <input
                      value={m.prefix || ''}
                      placeholder="如 fs (fs__tool)"
                      onChange={e => updateMember(idx, { prefix: e.target.value.trim() })}
                      style={{ fontFamily: 'var(--font-mono)' }}
                    />
                  </div>
                  <div style={{ alignSelf: 'center', paddingTop: 16 }}>
                    <span style={{ fontSize: 12, color: 'var(--text-2)' }}>
                      调用名称前缀：<code>{m.prefix ? `${m.prefix}__*` : '无前缀 (直接映射)'}</code>
                    </span>
                  </div>
                  <button
                    className="btn-icon"
                    title="移除成员"
                    onClick={() => removeMember(idx)}
                    style={{ alignSelf: 'center', marginTop: 16 }}
                  >
                    <IconTrash />
                  </button>
                </div>

                {/* Provider Capabilities Status Bar */}
                <div style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '6px 10px',
                  background: 'var(--bg-panel)',
                  borderRadius: 6,
                  border: '1px solid var(--border)',
                  fontSize: 12,
                  flexWrap: 'wrap',
                  gap: 8,
                }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                    {isLoading ? (
                      <span style={{ color: 'var(--text-2)', display: 'flex', alignItems: 'center', gap: 6 }}>
                        ⏳ 正在探测 {m.provider} 提供的工具、资源与提示词...
                      </span>
                    ) : caps?.success ? (
                      <>
                        <span style={{ color: 'var(--text-2)', fontWeight: 500 }}>上游已声明能力：</span>
                        <span className="badge" style={{ fontSize: 11, background: 'var(--accent-light)', color: 'var(--accent)' }}>
                          🛠️ {toolsList.length} 个工具
                        </span>
                        <span className="badge" style={{ fontSize: 11 }}>
                          📁 {resourcesList.length} 个资源
                        </span>
                        <span className="badge" style={{ fontSize: 11 }}>
                          💬 {promptsList.length} 个提示词
                        </span>
                        {caps.server_info && (
                          <span style={{ fontSize: 11, color: 'var(--text-3)' }}>
                            ({caps.server_info.name} v{caps.server_info.version || '1.0'})
                          </span>
                        )}
                      </>
                    ) : caps?.error ? (
                      <span style={{ color: 'var(--danger)', fontSize: 11 }}>
                        ⚠️ 探测上游元数据失败: {caps.error}
                      </span>
                    ) : (
                      <span style={{ color: 'var(--text-3)' }}>尚未获取到上游能力信息</span>
                    )}
                  </div>
                  <button
                    type="button"
                    className="btn"
                    style={{ fontSize: 11, padding: '2px 8px' }}
                    disabled={isLoading}
                    onClick={() => loadCapsForProvider(m.provider, true)}
                  >
                    {isLoading ? '探测中...' : '🔄 刷新上游能力'}
                  </button>
                </div>

                {/* Capabilities Filter Section with Category Tabs */}
                <div style={{ borderTop: '1px dashed var(--border)', paddingTop: 10, display: 'flex', flexDirection: 'column', gap: 10 }}>
                  <div style={{ display: 'flex', gap: 6, borderBottom: '1px solid var(--border)', paddingBottom: 6 }}>
                    <button
                      type="button"
                      onClick={() => setActiveCategory(prev => ({ ...prev, [idx]: 'tools' }))}
                      style={{
                        padding: '4px 10px',
                        borderRadius: 4,
                        border: 'none',
                        background: curCategory === 'tools' ? 'var(--accent)' : 'var(--bg-panel)',
                        color: curCategory === 'tools' ? '#fff' : 'var(--text-2)',
                        fontWeight: curCategory === 'tools' ? 600 : 400,
                        fontSize: 12,
                        cursor: 'pointer',
                      }}
                    >
                      🛠️ 工具 Tools ({isAllTools ? '全部' : `${memberTools.length}/${toolsList.length}`})
                    </button>
                    <button
                      type="button"
                      onClick={() => setActiveCategory(prev => ({ ...prev, [idx]: 'resources' }))}
                      style={{
                        padding: '4px 10px',
                        borderRadius: 4,
                        border: 'none',
                        background: curCategory === 'resources' ? 'var(--accent)' : 'var(--bg-panel)',
                        color: curCategory === 'resources' ? '#fff' : 'var(--text-2)',
                        fontWeight: curCategory === 'resources' ? 600 : 400,
                        fontSize: 12,
                        cursor: 'pointer',
                      }}
                    >
                      📁 资源 Resources ({isAllResources ? '全部' : `${memberResources.length}/${resourcesList.length}`})
                    </button>
                    <button
                      type="button"
                      onClick={() => setActiveCategory(prev => ({ ...prev, [idx]: 'prompts' }))}
                      style={{
                        padding: '4px 10px',
                        borderRadius: 4,
                        border: 'none',
                        background: curCategory === 'prompts' ? 'var(--accent)' : 'var(--bg-panel)',
                        color: curCategory === 'prompts' ? '#fff' : 'var(--text-2)',
                        fontWeight: curCategory === 'prompts' ? 600 : 400,
                        fontSize: 12,
                        cursor: 'pointer',
                      }}
                    >
                      💬 提示词 Prompts ({isAllPrompts ? '全部' : `${memberPrompts.length}/${promptsList.length}`})
                    </button>
                  </div>

                  {/* 1. Tools Tab */}
                  {curCategory === 'tools' && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, fontWeight: 600, cursor: 'pointer' }}>
                          <input
                            type="checkbox"
                            checked={isAllTools}
                            onChange={e => {
                              if (e.target.checked) {
                                updateMember(idx, { tools: ['*'] })
                              } else {
                                const allNames = toolsList.map(t => t.name)
                                updateMember(idx, { tools: allNames.length > 0 ? allNames : [] })
                              }
                            }}
                          />
                          <span>全部放行所有工具 (*)</span>
                        </label>

                        {!isAllTools && toolsList.length > 0 && (
                          <div style={{ display: 'flex', gap: 6 }}>
                            <button
                              type="button"
                              className="btn"
                              style={{ fontSize: 11, padding: '2px 8px' }}
                              onClick={() => updateMember(idx, { tools: toolsList.map(t => t.name) })}
                            >
                              全选
                            </button>
                            <button
                              type="button"
                              className="btn"
                              style={{ fontSize: 11, padding: '2px 8px' }}
                              onClick={() => updateMember(idx, { tools: [] })}
                            >
                              清空
                            </button>
                            <button
                              type="button"
                              className="btn"
                              style={{ fontSize: 11, padding: '2px 8px' }}
                              onClick={() => {
                                const cur = memberTools
                                const inverted = toolsList.map(t => t.name).filter(n => !cur.includes(n))
                                updateMember(idx, { tools: inverted })
                              }}
                            >
                              反选
                            </button>
                          </div>
                        )}
                      </div>

                      {toolsList.length > 0 ? (
                        <div style={{
                          display: 'grid',
                          gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
                          gap: 8,
                          maxHeight: 260,
                          overflowY: 'auto',
                          padding: 2,
                        }}>
                          {toolsList.map(t => {
                            const isChecked = isAllTools || memberTools.includes(t.name)
                            return (
                              <label
                                key={t.name}
                                style={{
                                  display: 'flex',
                                  alignItems: 'flex-start',
                                  gap: 8,
                                  padding: '8px 10px',
                                  background: isChecked ? 'var(--bg-hover)' : 'var(--bg-panel)',
                                  border: isChecked ? '1px solid var(--accent)' : '1px solid var(--border)',
                                  borderRadius: 6,
                                  cursor: isAllTools ? 'default' : 'pointer',
                                  opacity: isAllTools ? 0.85 : 1,
                                }}
                              >
                                <input
                                  type="checkbox"
                                  checked={isChecked}
                                  disabled={isAllTools}
                                  onChange={e => {
                                    const cur = memberTools.filter(x => x !== '*')
                                    const next = e.target.checked
                                      ? [...cur, t.name]
                                      : cur.filter(x => x !== t.name)
                                    updateMember(idx, { tools: next })
                                  }}
                                  style={{ marginTop: 3 }}
                                />
                                <div style={{ flex: 1, minWidth: 0 }}>
                                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
                                    <code style={{ fontWeight: 600, fontSize: 12, color: 'var(--text)' }}>
                                      {m.prefix ? `${m.prefix}__${t.name}` : t.name}
                                    </code>
                                    {m.prefix && (
                                      <span style={{ fontSize: 10, color: 'var(--text-3)' }}>({t.name})</span>
                                    )}
                                  </div>
                                  {t.description && (
                                    <div style={{
                                      fontSize: 11,
                                      color: 'var(--text-2)',
                                      marginTop: 3,
                                      lineHeight: 1.3,
                                      overflow: 'hidden',
                                      textOverflow: 'ellipsis',
                                      display: '-webkit-box',
                                      WebkitLineClamp: 2,
                                      WebkitBoxOrient: 'vertical',
                                    }}>
                                      {t.description}
                                    </div>
                                  )}
                                </div>
                              </label>
                            )
                          })}
                        </div>
                      ) : (
                        <div style={{ fontSize: 12, color: 'var(--text-3)', padding: '6px 0' }}>
                          {isLoading ? '正在加载上游工具列表...' : '该 Provider 尚未返回工具定义'}
                        </div>
                      )}

                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 4 }}>
                        <span style={{ fontSize: 11, color: 'var(--text-3)', whiteSpace: 'nowrap' }}>手动指定白名单:</span>
                        <input
                          value={memberTools.join(', ')}
                          placeholder="例如 * 或 tool1, tool2"
                          onChange={e => updateMember(idx, { tools: e.target.value.split(/[,\s]+/).filter(Boolean) })}
                          style={{ fontFamily: 'var(--font-mono)', fontSize: 12, height: 28 }}
                        />
                      </div>
                    </div>
                  )}

                  {/* 2. Resources Tab */}
                  {curCategory === 'resources' && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, fontWeight: 600, cursor: 'pointer' }}>
                          <input
                            type="checkbox"
                            checked={isAllResources}
                            onChange={e => {
                              if (e.target.checked) {
                                updateMember(idx, { resources: ['*'] })
                              } else {
                                const allKeys = resourcesList.map(r => r.uri || r.name)
                                updateMember(idx, { resources: allKeys.length > 0 ? allKeys : [] })
                              }
                            }}
                          />
                          <span>全部放行所有资源 (*)</span>
                        </label>

                        {!isAllResources && resourcesList.length > 0 && (
                          <div style={{ display: 'flex', gap: 6 }}>
                            <button
                              type="button"
                              className="btn"
                              style={{ fontSize: 11, padding: '2px 8px' }}
                              onClick={() => updateMember(idx, { resources: resourcesList.map(r => r.uri || r.name) })}
                            >
                              全选
                            </button>
                            <button
                              type="button"
                              className="btn"
                              style={{ fontSize: 11, padding: '2px 8px' }}
                              onClick={() => updateMember(idx, { resources: [] })}
                            >
                              清空
                            </button>
                          </div>
                        )}
                      </div>

                      {resourcesList.length > 0 ? (
                        <div style={{
                          display: 'grid',
                          gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
                          gap: 8,
                          maxHeight: 260,
                          overflowY: 'auto',
                          padding: 2,
                        }}>
                          {resourcesList.map(r => {
                            const key = r.uri || r.name
                            const isChecked = isAllResources || memberResources.includes(r.uri) || memberResources.includes(r.name)
                            return (
                              <label
                                key={key}
                                style={{
                                  display: 'flex',
                                  alignItems: 'flex-start',
                                  gap: 8,
                                  padding: '8px 10px',
                                  background: isChecked ? 'var(--bg-hover)' : 'var(--bg-panel)',
                                  border: isChecked ? '1px solid var(--accent)' : '1px solid var(--border)',
                                  borderRadius: 6,
                                  cursor: isAllResources ? 'default' : 'pointer',
                                  opacity: isAllResources ? 0.85 : 1,
                                }}
                              >
                                <input
                                  type="checkbox"
                                  checked={isChecked}
                                  disabled={isAllResources}
                                  onChange={e => {
                                    const cur = memberResources.filter(x => x !== '*')
                                    const next = e.target.checked
                                      ? [...cur, key]
                                      : cur.filter(x => x !== r.uri && x !== r.name)
                                    updateMember(idx, { resources: next })
                                  }}
                                  style={{ marginTop: 3 }}
                                />
                                <div style={{ flex: 1, minWidth: 0 }}>
                                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                                    <span style={{ fontWeight: 600, fontSize: 12 }}>{r.name || r.uri}</span>
                                    {r.mimeType && <span className="badge" style={{ fontSize: 10 }}>{r.mimeType}</span>}
                                  </div>
                                  <code style={{ fontSize: 11, color: 'var(--text-3)', wordBreak: 'break-all', display: 'block', marginTop: 2 }}>
                                    {r.uri}
                                  </code>
                                  {r.description && (
                                    <div style={{ fontSize: 11, color: 'var(--text-2)', marginTop: 2 }}>{r.description}</div>
                                  )}
                                </div>
                              </label>
                            )
                          })}
                        </div>
                      ) : (
                        <div style={{ fontSize: 12, color: 'var(--text-3)', padding: '6px 0' }}>
                          {isLoading ? '正在加载上游资源列表...' : '该 Provider 未声明可读取的 Resource 资源'}
                        </div>
                      )}

                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 4 }}>
                        <span style={{ fontSize: 11, color: 'var(--text-3)', whiteSpace: 'nowrap' }}>手动指定白名单:</span>
                        <input
                          value={memberResources.join(', ')}
                          placeholder="例如 * 或 file:///tmp/data.txt"
                          onChange={e => updateMember(idx, { resources: e.target.value.split(/[,\s]+/).filter(Boolean) })}
                          style={{ fontFamily: 'var(--font-mono)', fontSize: 12, height: 28 }}
                        />
                      </div>
                    </div>
                  )}

                  {/* 3. Prompts Tab */}
                  {curCategory === 'prompts' && (
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
                      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13, fontWeight: 600, cursor: 'pointer' }}>
                          <input
                            type="checkbox"
                            checked={isAllPrompts}
                            onChange={e => {
                              if (e.target.checked) {
                                updateMember(idx, { prompts: ['*'] })
                              } else {
                                const allNames = promptsList.map(p => p.name)
                                updateMember(idx, { prompts: allNames.length > 0 ? allNames : [] })
                              }
                            }}
                          />
                          <span>全部放行所有提示词 (*)</span>
                        </label>

                        {!isAllPrompts && promptsList.length > 0 && (
                          <div style={{ display: 'flex', gap: 6 }}>
                            <button
                              type="button"
                              className="btn"
                              style={{ fontSize: 11, padding: '2px 8px' }}
                              onClick={() => updateMember(idx, { prompts: promptsList.map(p => p.name) })}
                            >
                              全选
                            </button>
                            <button
                              type="button"
                              className="btn"
                              style={{ fontSize: 11, padding: '2px 8px' }}
                              onClick={() => updateMember(idx, { prompts: [] })}
                            >
                              清空
                            </button>
                          </div>
                        )}
                      </div>

                      {promptsList.length > 0 ? (
                        <div style={{
                          display: 'grid',
                          gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))',
                          gap: 8,
                          maxHeight: 260,
                          overflowY: 'auto',
                          padding: 2,
                        }}>
                          {promptsList.map(p => {
                            const isChecked = isAllPrompts || memberPrompts.includes(p.name)
                            return (
                              <label
                                key={p.name}
                                style={{
                                  display: 'flex',
                                  alignItems: 'flex-start',
                                  gap: 8,
                                  padding: '8px 10px',
                                  background: isChecked ? 'var(--bg-hover)' : 'var(--bg-panel)',
                                  border: isChecked ? '1px solid var(--accent)' : '1px solid var(--border)',
                                  borderRadius: 6,
                                  cursor: isAllPrompts ? 'default' : 'pointer',
                                  opacity: isAllPrompts ? 0.85 : 1,
                                }}
                              >
                                <input
                                  type="checkbox"
                                  checked={isChecked}
                                  disabled={isAllPrompts}
                                  onChange={e => {
                                    const cur = memberPrompts.filter(x => x !== '*')
                                    const next = e.target.checked
                                      ? [...cur, p.name]
                                      : cur.filter(x => x !== p.name)
                                    updateMember(idx, { prompts: next })
                                  }}
                                  style={{ marginTop: 3 }}
                                />
                                <div style={{ flex: 1, minWidth: 0 }}>
                                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                                    <code style={{ fontWeight: 600, fontSize: 12 }}>{p.name}</code>
                                    {p.arguments && p.arguments.length > 0 && (
                                      <span style={{ fontSize: 11, color: 'var(--text-3)' }}>({p.arguments.length} 个参数)</span>
                                    )}
                                  </div>
                                  {p.description && (
                                    <div style={{ fontSize: 11, color: 'var(--text-2)', marginTop: 2 }}>{p.description}</div>
                                  )}
                                </div>
                              </label>
                            )
                          })}
                        </div>
                      ) : (
                        <div style={{ fontSize: 12, color: 'var(--text-3)', padding: '6px 0' }}>
                          {isLoading ? '正在加载上游提示词列表...' : '该 Provider 未声明 Prompt 提示词模板'}
                        </div>
                      )}

                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 4 }}>
                        <span style={{ fontSize: 11, color: 'var(--text-3)', whiteSpace: 'nowrap' }}>手动指定白名单:</span>
                        <input
                          value={memberPrompts.join(', ')}
                          placeholder="例如 * 或 prompt1, prompt2"
                          onChange={e => updateMember(idx, { prompts: e.target.value.split(/[,\s]+/).filter(Boolean) })}
                          style={{ fontFamily: 'var(--font-mono)', fontSize: 12, height: 28 }}
                        />
                      </div>
                    </div>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </div>

      {/* 3. Downstream Endpoint Preview Card */}
      {c.name && (
        <div style={{
          background: 'var(--bg)',
          border: '1px solid var(--border)',
          borderRadius: 'var(--radius)',
          padding: '16px 18px',
        }}>
          <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 8 }}>
            下游调用接入端点
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8, fontSize: 12, fontFamily: 'var(--font-mono)' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', background: 'var(--bg-panel)', padding: '6px 10px', borderRadius: 4, border: '1px solid var(--border)' }}>
              <div>
                <span style={{ color: 'var(--accent)', fontWeight: 600 }}>Streamable HTTP:</span>{' '}
                <code>{origin}/mcp/{c.name}</code>
              </div>
              <button
                className="btn-icon"
                title="复制 cURL"
                onClick={() => {
                  const curl = `curl -X POST ${origin}/mcp/${c.name} -H "Content-Type: application/json" -H "${c.user_id_header || 'X-User-Id'}: user1" -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`
                  navigator.clipboard.writeText(curl)
                  alert('已复制 Streamable HTTP cURL 命令')
                }}
              >
                <IconCopy />
              </button>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', background: 'var(--bg-panel)', padding: '6px 10px', borderRadius: 4, border: '1px solid var(--border)' }}>
              <div>
                <span style={{ color: 'var(--accent)', fontWeight: 600 }}>SSE Stream:</span>{' '}
                <code>{origin}/mcp/{c.name}/sse</code>
              </div>
              <button
                className="btn-icon"
                title="复制 URL"
                onClick={() => {
                  navigator.clipboard.writeText(`${origin}/mcp/${c.name}/sse`)
                  alert('已复制 SSE 端点 URL')
                }}
              >
                <IconCopy />
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

// ── Main McpConfig Page ──────────────────────────────────────────
export default function McpConfig() {
  const [tab, setTab] = useState<'providers' | 'combos'>('providers')
  const [initialLoaded, setInitialLoaded] = useState(false)
  const [providers, setProviders] = useState<McpProviderConfig[]>([])
  const [combos, setCombos] = useState<McpComboConfig[]>([])
  const [origProviders, setOrigProviders] = useState('')
  const [origCombos, setOrigCombos] = useState('')

  const [selProvider, setSelProvider] = useState(0)
  const [selCombo, setSelCombo] = useState(0)

  const [providerSearch, setProviderSearch] = useState('')
  const [comboSearch, setComboSearch] = useState('')

  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [testResults, setTestResults] = useState<Record<string, McpTestResult | 'testing'>>({})

  const loadData = async () => {
    try {
      const data = await getMcpConfig()
      const provs = data.mcp_providers || []
      const cmbs = data.mcp_combos || []
      setProviders(provs)
      setCombos(cmbs)
      setOrigProviders(JSON.stringify(provs))
      setOrigCombos(JSON.stringify(cmbs))
      setInitialLoaded(true)
    } catch (e: any) {
      setErr('获取 MCP 配置失败: ' + (e.message || String(e)))
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  const isDirty = useMemo(() => {
    if (!initialLoaded) return false
    return JSON.stringify(providers) !== origProviders || JSON.stringify(combos) !== origCombos
  }, [initialLoaded, providers, combos, origProviders, origCombos])

  const sanitizeProviders = (list: McpProviderConfig[]): McpProviderConfig[] => {
    return list.map(p => {
      const clean: McpProviderConfig = {
        name: p.name.trim(),
        transport: p.transport,
        timeout_seconds: p.timeout_seconds || 30,
        auth_mode: p.auth_mode || 'shared',
      }
      if (p.transport === 'stdio') {
        clean.command = p.command?.trim()
        clean.args = p.args || []
        clean.env = p.env || {}
        if (p.working_dir) clean.working_dir = p.working_dir.trim()
      } else {
        clean.url = p.url?.trim()
        clean.headers = p.headers || {}
      }

      const mode = p.auth?.mode || p.auth?.type || 'none'
      const isIsolated = p.auth?.isolated || p.auth_mode === 'isolated'
      if (mode === 'api_key') {
        clean.auth = {
          mode: 'api_key',
          isolated: isIsolated,
          header: (p.auth?.header || p.auth?.header_name || 'Authorization').trim(),
          api_key: (p.auth?.api_key || p.auth?.header_value || '').trim(),
        }
      } else if (mode === 'oauth2') {
        clean.auth = {
          mode: 'oauth2',
          isolated: true,
          client_id: p.auth?.client_id?.trim() || '',
          client_secret: p.auth?.client_secret?.trim() || '',
          auth_url: (p.auth?.auth_url || p.auth?.authorization_url || '').trim(),
          token_url: p.auth?.token_url?.trim() || '',
          scopes: p.auth?.scopes || [],
        }
        if (p.auth?.redirect_url) {
          clean.auth.redirect_url = p.auth.redirect_url.trim()
        }
      } else if (isIsolated) {
        clean.auth = {
          mode: 'none',
          isolated: true,
        }
      } else {
        clean.auth = {
          mode: 'none',
          isolated: false,
        }
      }
      return clean
    })
  }

  const sanitizeCombos = (list: McpComboConfig[]): McpComboConfig[] => {
    return list.map(c => ({
      name: c.name.trim(),
      description: c.description?.trim(),
      owned_by: c.owned_by?.trim(),
      user_id_header: c.user_id_header?.trim() || 'X-User-Id',
      members: (c.members || []).map(m => {
        const mm: McpComboMemberConfig = {
          provider: m.provider.trim(),
        }
        if (m.prefix && m.prefix.trim() !== '') {
          mm.prefix = m.prefix.trim()
        }
        if (m.tools && m.tools.length > 0) {
          mm.tools = m.tools
        } else {
          mm.tools = ['*']
        }
        if (m.resources && m.resources.length > 0) {
          mm.resources = m.resources
        }
        if (m.prompts && m.prompts.length > 0) {
          mm.prompts = m.prompts
        }
        return mm
      }),
    }))
  }

  const save = async () => {
    setSaving(true)
    setMsg('')
    setErr('')
    try {
      const cleanProviders = sanitizeProviders(providers)
      const cleanCombos = sanitizeCombos(combos)
      const res = await putMcpConfig({
        mcp_providers: cleanProviders,
        mcp_combos: cleanCombos,
      })
      const newProvs = res.mcp_providers || []
      const newCmbs = res.mcp_combos || []
      setProviders(newProvs)
      setCombos(newCmbs)
      setOrigProviders(JSON.stringify(newProvs))
      setOrigCombos(JSON.stringify(newCmbs))
      setMsg('MCP 配置已成功保存并热重载生效！')
    } catch (e: any) {
      setErr(e.message || String(e))
    } finally {
      setSaving(false)
    }
  }

  const handleTestProvider = async (name: string) => {
    if (!name) return
    setTestResults(prev => ({ ...prev, [name]: 'testing' }))
    try {
      const res = await testMcpProvider(name)
      setTestResults(prev => ({ ...prev, [name]: res }))
    } catch (err: any) {
      setTestResults(prev => ({ ...prev, [name]: { success: false, error: err.message } }))
    }
  }

  // Filtered lists
  const filteredProviders = useMemo(() => {
    const q = providerSearch.toLowerCase().trim()
    return providers.filter(p => !q || p.name.toLowerCase().includes(q) || p.transport.toLowerCase().includes(q))
  }, [providers, providerSearch])

  const filteredCombos = useMemo(() => {
    const q = comboSearch.toLowerCase().trim()
    return combos.filter(c => !q || c.name.toLowerCase().includes(q) || (c.description || '').toLowerCase().includes(q))
  }, [combos, comboSearch])

  const curProvider = providers[selProvider]
  const curCombo = combos[selCombo]

  return (
    <div className="page" style={{ paddingBottom: 80, display: 'flex', flexDirection: 'column', height: '100%' }}>
      {/* Header */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 16, marginBottom: 20, paddingBottom: 16, borderBottom: '1px solid var(--border)' }}>
        <div>
          <span style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.01em', marginRight: 12 }}>MCP 网关配置</span>
          <span style={{ fontSize: 13, color: 'var(--text-2)' }}>可视化配置上游 Provider 与虚拟 Combo 工具箱，支持免重启热重载</span>
          {isDirty && (
            <span className="badge badge-outline" style={{ marginLeft: 10, fontSize: 11, color: 'var(--warning)', borderColor: 'var(--warning)' }}>
              未保存改动
            </span>
          )}
        </div>
        <button
          className="btn-primary"
          onClick={save}
          disabled={saving || !initialLoaded}
          style={{ marginLeft: 'auto', minWidth: 120 }}
        >
          {saving ? '保存中...' : '保存并热重载'}
        </button>
      </div>

      {msg && <div className="alert ok" style={{ marginBottom: 12 }}>✓ {msg}</div>}
      {err && <div className="alert err" style={{ marginBottom: 12 }}>{err}</div>}

      {/* Tab Switcher */}
      <div style={{ display: 'flex', gap: 2, marginBottom: 16, background: 'var(--bg)', padding: 4, borderRadius: 8, border: '1px solid var(--border)', width: 'fit-content' }}>
        <button
          onClick={() => setTab('providers')}
          style={{
            padding: '6px 16px',
            borderRadius: 6,
            border: 'none',
            background: tab === 'providers' ? 'var(--bg-panel)' : 'transparent',
            color: tab === 'providers' ? 'var(--text)' : 'var(--text-2)',
            fontWeight: tab === 'providers' ? 600 : 400,
            fontSize: 13,
            boxShadow: tab === 'providers' ? '0 1px 3px rgba(0,0,0,0.1)' : 'none',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: 6,
          }}
        >
          ⚡ Providers
          <span style={{
            fontSize: 11, padding: '1px 6px', borderRadius: 10,
            background: tab === 'providers' ? 'var(--accent-light)' : 'var(--bg-code)',
            color: tab === 'providers' ? 'var(--accent)' : 'var(--text-3)',
            fontWeight: 500,
          }}>{providers.length}</span>
        </button>
        <button
          onClick={() => setTab('combos')}
          style={{
            padding: '6px 16px',
            borderRadius: 6,
            border: 'none',
            background: tab === 'combos' ? 'var(--bg-panel)' : 'transparent',
            color: tab === 'combos' ? 'var(--text)' : 'var(--text-2)',
            fontWeight: tab === 'combos' ? 600 : 400,
            fontSize: 13,
            boxShadow: tab === 'combos' ? '0 1px 3px rgba(0,0,0,0.1)' : 'none',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            gap: 6,
          }}
        >
          📁 Combos
          <span style={{
            fontSize: 11, padding: '1px 6px', borderRadius: 10,
            background: tab === 'combos' ? 'var(--accent-light)' : 'var(--bg-code)',
            color: tab === 'combos' ? 'var(--accent)' : 'var(--text-3)',
            fontWeight: 500,
          }}>{combos.length}</span>
        </button>
      </div>

      {/* Master-Detail Layout */}
      <div style={{
        display: 'flex',
        flex: 1,
        background: 'var(--bg-panel)',
        border: '1px solid var(--border)',
        borderRadius: 10,
        overflow: 'hidden',
        boxShadow: '0 1px 3px rgba(0,0,0,0.08)',
      }}>
        {/* Left Sidebar List */}
        <div style={{
          width: 260,
          minWidth: 260,
          borderRight: '1px solid var(--border)',
          display: 'flex',
          flexDirection: 'column',
          background: 'var(--bg)',
        }}>
          <div style={{ padding: '12px 12px 8px', display: 'flex', gap: 8 }}>
            <input
              value={tab === 'providers' ? providerSearch : comboSearch}
              onChange={e => tab === 'providers' ? setProviderSearch(e.target.value) : setComboSearch(e.target.value)}
              placeholder="搜索..."
              style={{ fontSize: 12, flex: 1 }}
            />
            <button
              className="btn"
              title="新增"
              onClick={() => {
                if (tab === 'providers') {
                  const np = EMPTY_MCP_PROVIDER()
                  np.name = `provider-${providers.length + 1}`
                  setProviders([...providers, np])
                  setSelProvider(providers.length)
                } else {
                  const nc = EMPTY_MCP_COMBO()
                  nc.name = `combo-${combos.length + 1}`
                  setCombos([...combos, nc])
                  setSelCombo(combos.length)
                }
              }}
              style={{ padding: '4px 8px' }}
            >
              <IconPlus />
            </button>
          </div>

          <div style={{ flex: 1, overflowY: 'auto', padding: '4px 8px 12px' }}>
            {tab === 'providers' ? (
              filteredProviders.length === 0 ? (
                <div style={{ padding: 16, fontSize: 12, color: 'var(--text-3)', textAlign: 'center' }}>暂无 Provider</div>
              ) : (
                filteredProviders.map(p => {
                  const realIdx = providers.indexOf(p)
                  const isSelected = realIdx === selProvider
                  return (
                    <div
                      key={p.name + realIdx}
                      onClick={() => setSelProvider(realIdx)}
                      style={{
                        padding: '10px 12px',
                        marginBottom: 4,
                        borderRadius: 6,
                        cursor: 'pointer',
                        background: isSelected ? 'var(--bg-panel)' : 'transparent',
                        border: isSelected ? '1px solid var(--border)' : '1px solid transparent',
                        boxShadow: isSelected ? '0 1px 3px rgba(0,0,0,0.06)' : 'none',
                        display: 'flex',
                        justifyContent: 'space-between',
                        alignItems: 'center',
                      }}
                    >
                      <div>
                        <div style={{ fontWeight: isSelected ? 600 : 500, fontSize: 13, color: 'var(--text)' }}>
                          {p.name || '未命名 Provider'}
                        </div>
                        <div style={{ display: 'flex', gap: 6, marginTop: 4 }}>
                          <span className="badge" style={{ fontSize: 10 }}>{p.transport}</span>
                          {p.auth?.mode === 'oauth2' && <span className="badge badge-outline" style={{ fontSize: 10 }}>OAuth</span>}
                        </div>
                      </div>
                      <button
                        className="btn-icon"
                        title="删除"
                        onClick={e => {
                          e.stopPropagation()
                          if (confirm(`确认删除 Provider "${p.name}"？`)) {
                            const next = providers.filter((_, i) => i !== realIdx)
                            setProviders(next)
                            setSelProvider(Math.max(0, realIdx - 1))
                          }
                        }}
                      >
                        <IconTrash />
                      </button>
                    </div>
                  )
                })
              )
            ) : (
              filteredCombos.length === 0 ? (
                <div style={{ padding: 16, fontSize: 12, color: 'var(--text-3)', textAlign: 'center' }}>暂无 Combo</div>
              ) : (
                filteredCombos.map(c => {
                  const realIdx = combos.indexOf(c)
                  const isSelected = realIdx === selCombo
                  return (
                    <div
                      key={c.name + realIdx}
                      onClick={() => setSelCombo(realIdx)}
                      style={{
                        padding: '10px 12px',
                        marginBottom: 4,
                        borderRadius: 6,
                        cursor: 'pointer',
                        background: isSelected ? 'var(--bg-panel)' : 'transparent',
                        border: isSelected ? '1px solid var(--border)' : '1px solid transparent',
                        boxShadow: isSelected ? '0 1px 3px rgba(0,0,0,0.06)' : 'none',
                        display: 'flex',
                        justifyContent: 'space-between',
                        alignItems: 'center',
                      }}
                    >
                      <div>
                        <div style={{ fontWeight: isSelected ? 600 : 500, fontSize: 13, color: 'var(--text)' }}>
                          {c.name || '未命名 Combo'}
                        </div>
                        <div style={{ fontSize: 11, color: 'var(--text-3)', marginTop: 2 }}>
                          {(c.members || []).length} 个上游成员
                        </div>
                      </div>
                      <button
                        className="btn-icon"
                        title="删除"
                        onClick={e => {
                          e.stopPropagation()
                          if (confirm(`确认删除 Combo "${c.name}"？`)) {
                            const next = combos.filter((_, i) => i !== realIdx)
                            setCombos(next)
                            setSelCombo(Math.max(0, realIdx - 1))
                          }
                        }}
                      >
                        <IconTrash />
                      </button>
                    </div>
                  )
                })
              )
            )}
          </div>
        </div>

        {/* Right Detail Panel */}
        <div style={{ flex: 1, overflowY: 'auto' }}>
          {tab === 'providers' ? (
            curProvider ? (
              <ProviderDetail
                p={curProvider}
                onUpdate={patch => {
                  const next = [...providers]
                  next[selProvider] = { ...curProvider, ...patch }
                  setProviders(next)
                }}
                onTest={() => handleTestProvider(curProvider.name)}
                testState={testResults[curProvider.name]}
              />
            ) : (
              <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-3)' }}>
                请从左侧选择或添加一个 Provider
              </div>
            )
          ) : (
            curCombo ? (
              <ComboDetail
                c={curCombo}
                allProviders={providers}
                onUpdate={patch => {
                  const next = [...combos]
                  next[selCombo] = { ...curCombo, ...patch }
                  setCombos(next)
                }}
              />
            ) : (
              <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-3)' }}>
                请从左侧选择或添加一个 Combo
              </div>
            )
          )}
        </div>
      </div>
    </div>
  )
}
