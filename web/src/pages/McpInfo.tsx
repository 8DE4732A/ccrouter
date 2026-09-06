import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { getMcpAdminInfo, getMcpProviderCapabilities } from '../api/client'
import type { McpAdminInfo, McpCapabilitiesResult } from '../api/client'

const TRANSPORT_COLOR: Record<string, { fg: string; bg: string; border: string }> = {
  stdio: { fg: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.08)', border: 'rgba(139, 92, 246, 0.25)' },
  sse: { fg: '#0284c7', bg: 'rgba(2, 132, 199, 0.08)', border: 'rgba(2, 132, 199, 0.25)' },
  streamablehttp: { fg: '#10b981', bg: 'rgba(16, 185, 129, 0.08)', border: 'rgba(16, 185, 129, 0.25)' },
}

export default function McpInfoPage() {
  const [info, setInfo] = useState<McpAdminInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [err, setErr] = useState('')
  const [expandedComboTools, setExpandedComboTools] = useState<Record<string, boolean>>({})
  const [activeClientTab, setActiveClientTab] = useState<Record<string, 'curl' | 'claude' | 'cursor'>>({})
  const [copiedKey, setCopiedKey] = useState<string | null>(null)

  // Inspection drawer state for providers
  const [inspectingProvider, setInspectingProvider] = useState<string | null>(null)
  const [inspectResult, setInspectResult] = useState<McpCapabilitiesResult | null>(null)
  const [inspectLoading, setInspectLoading] = useState(false)
  const [inspectError, setInspectError] = useState('')

  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const loadInfo = () => {
    return getMcpAdminInfo()
      .then(res => {
        setInfo(res)
        setErr('')
      })
      .catch(e => setErr(String(e)))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    loadInfo()
    // Auto-refresh every 15 seconds
    timerRef.current = setInterval(loadInfo, 15_000)
    return () => {
      if (timerRef.current) clearInterval(timerRef.current)
    }
  }, [])

  const copyText = (key: string, text: string) => {
    navigator.clipboard.writeText(text)
    setCopiedKey(key)
    setTimeout(() => setCopiedKey(null), 2000)
  }

  const handleInspectProvider = async (name: string) => {
    setInspectingProvider(name)
    setInspectResult(null)
    setInspectError('')
    setInspectLoading(true)
    try {
      const res = await getMcpProviderCapabilities(name)
      setInspectResult(res)
    } catch (e: any) {
      setInspectError(e?.message || String(e))
    } finally {
      setInspectLoading(false)
    }
  }

  if (loading && !info) {
    return (
      <div className="page">
        <div className="empty-state">加载 MCP 运行信息中…</div>
      </div>
    )
  }

  if (err && !info) {
    return (
      <div className="page">
        <div className="alert err">{err}</div>
      </div>
    )
  }

  const baseOrigin = window.location.origin
  const externalUrl = info?.external_url?.replace(/\/+$/, '') || baseOrigin

  return (
    <div className="page">
      <div className="page-header" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <h1 className="page-title">MCP 信息</h1>
          <p style={{ color: 'var(--text-3)', fontSize: 13, marginTop: 4 }}>
            Model Context Protocol 聚合网关运行状态、端点协议、暴露工具与上游能力总览
          </p>
        </div>
        <button className="btn" onClick={() => loadInfo()} title="刷新状态">
          🔄 刷新
        </button>
      </div>

      {err && <div className="alert err" style={{ marginBottom: 20 }}>{err}</div>}

      {/* Top Stat Cards */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(180px, 1fr))', gap: 12, marginBottom: 28 }}>
        <div className="stat-card">
          <div className="stat-label">应用版本</div>
          <div className="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: 20 }}>
            v{info?.version}
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-label">运行时</div>
          <div className="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: 20 }}>
            {info?.runtime}
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-label">MCP Combos</div>
          <div className="stat-value">{info?.combos.length ?? 0}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">MCP Providers</div>
          <div className="stat-value">{info?.providers.length ?? 0}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">聚合生效工具</div>
          <div className="stat-value" style={{ color: 'var(--accent)' }}>
            {info?.total_tools ?? 0}
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-label">隔离凭据用户</div>
          <div className="stat-value" style={{ fontSize: 18 }}>
            {info?.user_count ?? 0} <span style={{ fontSize: 12, color: 'var(--text-3)', fontWeight: 400 }}>({info?.tokens_count ?? 0} 令牌)</span>
          </div>
        </div>
      </div>

      {/* Combos (聚合工具箱) */}
      <section style={{ marginBottom: 28 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
          <h2 style={{ fontSize: 13, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.07em', color: 'var(--text-3)' }}>
            可用工具箱 (Combos)
          </h2>
          <Link to="/mcp/config" className="btn btn-sm" style={{ fontSize: 12, color: 'var(--text-2)' }}>
            ⚙️ 配置 Combos
          </Link>
        </div>

        {(!info?.combos || info.combos.length === 0) ? (
          <div className="empty-state" style={{ background: 'var(--bg-panel)', border: '1px solid var(--border)', borderRadius: 8, padding: '24px' }}>
            尚未配置 MCP Combos。请前往 <Link to="/mcp/config" style={{ color: 'var(--accent)' }}>配置页面</Link> 添加聚合组合。
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
            {info.combos.map(c => {
              const fullHttpEndpoint = `${externalUrl}/mcp/${c.name}`
              const fullSseEndpoint = `${externalUrl}/mcp/${c.name}/sse`
              const isToolsExpanded = !!expandedComboTools[c.name]
              const currentClientTab = activeClientTab[c.name] || 'curl'

              // Code snippets
              const curlExample = `curl -X POST '${fullHttpEndpoint}' \\
  -H 'Content-Type: application/json' \\
  -H '${c.user_id_header}: user_123' \\
  -d '{"jsonrpc":"2.0","method":"tools/list","id":1}'`

              const claudeDesktopConfig = JSON.stringify({
                mcpServers: {
                  [c.name]: {
                    url: fullSseEndpoint,
                    headers: {
                      [c.user_id_header]: "your_user_id"
                    }
                  }
                }
              }, null, 2)

              const cursorConfig = JSON.stringify({
                name: c.name,
                type: "sse",
                url: fullSseEndpoint,
                headers: {
                  [c.user_id_header]: "your_user_id"
                }
              }, null, 2)

              return (
                <div
                  key={c.name}
                  style={{
                    background: 'var(--bg-panel)',
                    border: '1px solid var(--border)',
                    borderRadius: 8,
                    padding: '16px 20px',
                  }}
                >
                  <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap', marginBottom: 12 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' }}>
                      <code
                        style={{
                          fontFamily: 'var(--font-mono)',
                          fontSize: 15,
                          fontWeight: 600,
                          color: 'var(--accent)',
                          background: 'var(--accent-light)',
                          padding: '3px 10px',
                          borderRadius: 4,
                        }}
                      >
                        {c.name}
                      </code>
                      <span className="tag" style={{ fontSize: 11, color: 'var(--text-3)', background: 'var(--bg)' }}>
                        {c.owned_by || 'default'}
                      </span>
                      <span className="tag" style={{ fontSize: 11, color: '#10b981', background: 'rgba(16, 185, 129, 0.1)' }}>
                        Streamable HTTP
                      </span>
                      <span className="tag" style={{ fontSize: 11, color: '#0284c7', background: 'rgba(2, 132, 199, 0.1)' }}>
                        SSE
                      </span>
                      <span className="tag" style={{ fontSize: 11, color: 'var(--text-2)', background: 'var(--bg)' }}>
                        Header: <code>{c.user_id_header}</code>
                      </span>
                    </div>

                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <Link to={`/mcp/test?combo=${encodeURIComponent(c.name)}`} className="btn btn-sm" style={{ fontSize: 12 }}>
                        🧪 在 Playground 测试
                      </Link>
                    </div>
                  </div>

                  {c.description && (
                    <div style={{ fontSize: 13, color: 'var(--text-2)', marginBottom: 12 }}>
                      {c.description}
                    </div>
                  )}

                  {/* Members */}
                  <div style={{ background: 'var(--bg)', border: '1px solid var(--border-md)', borderRadius: 6, padding: '10px 14px', marginBottom: 12 }}>
                    <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-3)', textTransform: 'uppercase', marginBottom: 8 }}>
                      聚合上游成员 ({c.members.length})
                    </div>
                    <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                      {c.members.map((m, idx) => (
                        <div key={idx} style={{ display: 'flex', alignItems: 'center', gap: 10, fontSize: 12, flexWrap: 'wrap' }}>
                          <span
                            style={{
                              width: 18,
                              height: 18,
                              borderRadius: '50%',
                              background: 'var(--bg-panel)',
                              border: '1px solid var(--border-md)',
                              display: 'inline-flex',
                              alignItems: 'center',
                              justifyContent: 'center',
                              fontFamily: 'var(--font-mono)',
                              fontSize: 10,
                              color: 'var(--text-3)',
                              flexShrink: 0,
                            }}
                          >
                            {idx + 1}
                          </span>
                          <span style={{ fontWeight: 600, color: 'var(--text)' }}>{m.provider}</span>
                          {m.prefix ? (
                            <span className="tag" style={{ fontSize: 11, color: '#8b5cf6', background: 'rgba(139, 92, 246, 0.1)' }}>
                              前缀: {m.prefix}__
                            </span>
                          ) : (
                            <span className="tag" style={{ fontSize: 10, color: 'var(--text-3)' }}>
                              无前缀
                            </span>
                          )}
                          <span style={{ color: 'var(--border-md)' }}>|</span>
                          <span style={{ color: 'var(--text-3)' }}>
                            工具: {(!m.tools || m.tools.length === 0 || m.tools.includes('*')) ? (
                              <span style={{ color: 'var(--ok-fg)' }}>全部放行 (*)</span>
                            ) : (
                              `${m.tools.length} 个白名单 (${m.tools.slice(0, 3).join(', ')}${m.tools.length > 3 ? '...' : ''})`
                            )}
                          </span>
                          {m.resources && m.resources.length > 0 && (
                            <>
                              <span style={{ color: 'var(--border-md)' }}>|</span>
                              <span style={{ color: 'var(--text-3)' }}>
                                资源: {m.resources.includes('*') ? '全部放行 (*)' : `${m.resources.length} 个`}
                              </span>
                            </>
                          )}
                          {m.prompts && m.prompts.length > 0 && (
                            <>
                              <span style={{ color: 'var(--border-md)' }}>|</span>
                              <span style={{ color: 'var(--text-3)' }}>
                                提示词: {m.prompts.includes('*') ? '全部放行 (*)' : `${m.prompts.length} 个`}
                              </span>
                            </>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>

                  {/* Exposed Tools */}
                  <div style={{ marginBottom: 14 }}>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 6 }}>
                      <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-2)' }}>
                        当前对外暴露生效工具：
                        <span style={{ color: c.tool_count > 0 ? 'var(--accent)' : 'var(--text-3)', fontWeight: 700, marginLeft: 4 }}>
                          {c.tool_count} 个
                        </span>
                      </div>
                      {c.tool_count > 0 && (
                        <button
                          className="btn btn-sm"
                          style={{ fontSize: 11, padding: '2px 8px' }}
                          onClick={() => setExpandedComboTools(prev => ({ ...prev, [c.name]: !prev[c.name] }))}
                        >
                          {isToolsExpanded ? '收起工具列表 ▲' : '展开查看工具清单 ▼'}
                        </button>
                      )}
                    </div>
                    {isToolsExpanded && (
                      <div
                        style={{
                          background: 'var(--bg)',
                          border: '1px solid var(--border)',
                          borderRadius: 6,
                          padding: '10px 12px',
                          display: 'flex',
                          flexWrap: 'wrap',
                          gap: 6,
                          maxHeight: 180,
                          overflowY: 'auto',
                        }}
                      >
                        {c.tools.map(t => (
                          <code
                            key={t}
                            style={{
                              fontFamily: 'var(--font-mono)',
                              fontSize: 12,
                              color: 'var(--text)',
                              background: 'var(--bg-panel)',
                              border: '1px solid var(--border-md)',
                              padding: '2px 8px',
                              borderRadius: 4,
                            }}
                          >
                            {t}
                          </code>
                        ))}
                      </div>
                    )}
                  </div>

                  {/* Client Integration Snippets */}
                  <div style={{ borderTop: '1px solid var(--border)', paddingTop: 12 }}>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
                      <div style={{ display: 'flex', gap: 6 }}>
                        <button
                          className={`btn btn-sm ${currentClientTab === 'curl' ? 'btn-primary' : ''}`}
                          style={{ fontSize: 11, padding: '2px 8px' }}
                          onClick={() => setActiveClientTab(prev => ({ ...prev, [c.name]: 'curl' }))}
                        >
                          cURL 示例
                        </button>
                        <button
                          className={`btn btn-sm ${currentClientTab === 'claude' ? 'btn-primary' : ''}`}
                          style={{ fontSize: 11, padding: '2px 8px' }}
                          onClick={() => setActiveClientTab(prev => ({ ...prev, [c.name]: 'claude' }))}
                        >
                          Claude Desktop 配置
                        </button>
                        <button
                          className={`btn btn-sm ${currentClientTab === 'cursor' ? 'btn-primary' : ''}`}
                          style={{ fontSize: 11, padding: '2px 8px' }}
                          onClick={() => setActiveClientTab(prev => ({ ...prev, [c.name]: 'cursor' }))}
                        >
                          Cursor / Cline 配置
                        </button>
                      </div>
                      <button
                        className="btn btn-sm"
                        style={{ fontSize: 11, padding: '2px 8px' }}
                        onClick={() => {
                          const text =
                            currentClientTab === 'curl'
                              ? curlExample
                              : currentClientTab === 'claude'
                              ? claudeDesktopConfig
                              : cursorConfig
                          copyText(`${c.name}-${currentClientTab}`, text)
                        }}
                      >
                        {copiedKey === `${c.name}-${currentClientTab}` ? '✓ 已复制' : '📋 复制配置'}
                      </button>
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
                        overflowX: 'auto',
                        color: 'var(--text-2)',
                      }}
                    >
                      {currentClientTab === 'curl'
                        ? curlExample
                        : currentClientTab === 'claude'
                        ? claudeDesktopConfig
                        : cursorConfig}
                    </pre>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>

      {/* Providers (上游服务) */}
      <section style={{ marginBottom: 28 }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
          <h2 style={{ fontSize: 13, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.07em', color: 'var(--text-3)' }}>
            上游服务 (Providers)
          </h2>
          <Link to="/mcp/config" className="btn btn-sm" style={{ fontSize: 12, color: 'var(--text-2)' }}>
            ⚙️ 配置 Providers
          </Link>
        </div>

        {(!info?.providers || info.providers.length === 0) ? (
          <div className="empty-state" style={{ background: 'var(--bg-panel)', border: '1px solid var(--border)', borderRadius: 8, padding: '24px' }}>
            尚未配置 MCP Providers。请前往 <Link to="/mcp/config" style={{ color: 'var(--accent)' }}>配置页面</Link> 添加上游服务。
          </div>
        ) : (
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: 12 }}>
            {info.providers.map(p => {
              const styleTheme = TRANSPORT_COLOR[p.transport] || TRANSPORT_COLOR.stdio
              return (
                <div
                  key={p.name}
                  style={{
                    background: 'var(--bg-panel)',
                    border: '1px solid var(--border)',
                    borderRadius: 8,
                    padding: '16px 18px',
                    display: 'flex',
                    flexDirection: 'column',
                    justifyContent: 'space-between',
                  }}
                >
                  <div>
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 8 }}>
                      <span style={{ fontWeight: 600, fontSize: 15 }}>{p.name}</span>
                      <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
                        <span
                          className="tag"
                          style={{
                            fontSize: 10,
                            fontWeight: 600,
                            color: styleTheme.fg,
                            background: styleTheme.bg,
                            border: `1px solid ${styleTheme.border}`,
                          }}
                        >
                          {p.transport}
                        </span>
                        <span
                          className="tag"
                          style={{
                            fontSize: 10,
                            color: p.is_ready ? 'var(--ok-fg)' : 'var(--text-3)',
                            background: p.is_ready ? 'var(--ok-bg, rgba(16,185,129,0.1))' : 'var(--bg)',
                          }}
                        >
                          {p.is_ready ? '就绪' : '未连接'}
                        </span>
                      </div>
                    </div>

                    {/* Target details */}
                    <div style={{ fontSize: 12, color: 'var(--text-2)', marginBottom: 8 }}>
                      {p.transport === 'stdio' ? (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                          <div>
                            <span style={{ color: 'var(--text-3)' }}>执行命令: </span>
                            <code style={{ fontFamily: 'var(--font-mono)' }}>{p.command}</code>
                          </div>
                          {p.args && p.args.length > 0 && (
                            <div>
                              <span style={{ color: 'var(--text-3)' }}>参数: </span>
                              <code style={{ fontFamily: 'var(--font-mono)', fontSize: 11 }}>{p.args.join(' ')}</code>
                            </div>
                          )}
                          {p.working_dir && (
                            <div style={{ color: 'var(--text-3)', fontSize: 11 }}>工作目录: {p.working_dir}</div>
                          )}
                        </div>
                      ) : (
                        <div>
                          <span style={{ color: 'var(--text-3)' }}>URL: </span>
                          <code style={{ fontFamily: 'var(--font-mono)', wordBreak: 'break-all' }}>{p.url || '-'}</code>
                        </div>
                      )}
                    </div>

                    {/* Auth & Timeout */}
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'center', marginBottom: 12 }}>
                      <span className="tag" style={{ fontSize: 10 }}>
                        鉴权: {p.auth_mode}
                      </span>
                      {p.isolated && (
                        <span className="tag" style={{ fontSize: 10, color: '#f59e0b', background: 'rgba(245, 158, 11, 0.1)' }}>
                          用户隔离认证
                        </span>
                      )}
                      <span className="tag" style={{ fontSize: 10, color: 'var(--text-3)' }}>
                        超时: {p.timeout_seconds || 30}s
                      </span>
                      {p.headers && p.headers.length > 0 && (
                        <span className="tag" style={{ fontSize: 10, color: 'var(--text-3)' }}>
                          自定义 Headers: {p.headers.length}
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Actions */}
                  <div style={{ borderTop: '1px solid var(--border)', paddingTop: 10, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span style={{ fontSize: 11, color: 'var(--text-3)' }}>
                      凭据令牌: {info.provider_tokens[p.name] ?? 0}
                    </span>
                    <button
                      className="btn btn-sm"
                      style={{ fontSize: 11 }}
                      onClick={() => handleInspectProvider(p.name)}
                    >
                      🔍 连通性与能力探测
                    </button>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </section>

      {/* User Token Stats */}
      {info && (info.tokens_count > 0 || Object.keys(info.provider_tokens).length > 0) && (
        <section style={{ marginBottom: 28 }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 12 }}>
            <h2 style={{ fontSize: 13, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.07em', color: 'var(--text-3)' }}>
              用户隔离凭据状态 (User Tokens)
            </h2>
            <Link to="/mcp/tokens" className="btn btn-sm" style={{ fontSize: 12, color: 'var(--text-2)' }}>
              🔑 凭据管理 ➜
            </Link>
          </div>
          <div
            style={{
              background: 'var(--bg-panel)',
              border: '1px solid var(--border)',
              borderRadius: 8,
              padding: '16px 20px',
            }}
          >
            <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap', alignItems: 'center', marginBottom: 12 }}>
              <div>
                <span style={{ fontSize: 12, color: 'var(--text-3)' }}>总授权用户数: </span>
                <strong style={{ fontSize: 16, marginLeft: 4 }}>{info.user_count}</strong>
              </div>
              <div>
                <span style={{ fontSize: 12, color: 'var(--text-3)' }}>活跃令牌数: </span>
                <strong style={{ fontSize: 16, marginLeft: 4, color: 'var(--accent)' }}>{info.tokens_count}</strong>
              </div>
            </div>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              {Object.entries(info.provider_tokens).map(([prov, count]) => (
                <span
                  key={prov}
                  className="tag"
                  style={{
                    fontSize: 12,
                    padding: '4px 10px',
                    background: 'var(--bg)',
                    border: '1px solid var(--border-md)',
                  }}
                >
                  <strong style={{ color: 'var(--text)' }}>{prov}</strong>: {count} 个用户令牌
                </span>
              ))}
            </div>
          </div>
        </section>
      )}

      {/* Capability Inspection Modal / Drawer */}
      {inspectingProvider && (
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
          onClick={() => setInspectingProvider(null)}
        >
          <div
            style={{
              background: 'var(--bg-panel)',
              border: '1px solid var(--border)',
              borderRadius: 10,
              width: '100%',
              maxWidth: 680,
              maxHeight: '85vh',
              display: 'flex',
              flexDirection: 'column',
              boxShadow: '0 20px 40px rgba(0,0,0,0.2)',
              overflow: 'hidden',
            }}
            onClick={e => e.stopPropagation()}
          >
            <div
              style={{
                padding: '14px 18px',
                borderBottom: '1px solid var(--border)',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <span style={{ fontWeight: 600, fontSize: 15 }}>
                  Provider 能力探测: <code style={{ color: 'var(--accent)' }}>{inspectingProvider}</code>
                </span>
              </div>
              <button className="btn btn-sm" onClick={() => setInspectingProvider(null)}>
                ✕ 关闭
              </button>
            </div>

            <div style={{ padding: '16px 20px', overflowY: 'auto', flex: 1 }}>
              {inspectLoading && (
                <div className="empty-state" style={{ padding: 40 }}>
                  正在连接上游服务并初始化 MCP 握手…
                </div>
              )}

              {inspectError && (
                <div className="alert err" style={{ marginBottom: 12 }}>
                  探测失败: {inspectError}
                </div>
              )}

              {inspectResult && (
                <div>
                  {inspectResult.server_info && (
                    <div style={{ background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: 6, padding: '10px 14px', marginBottom: 14 }}>
                      <div style={{ fontSize: 11, fontWeight: 600, color: 'var(--text-3)', textTransform: 'uppercase', marginBottom: 4 }}>
                        Server Info (上游服务信息)
                      </div>
                      <div style={{ fontSize: 13, color: 'var(--text)' }}>
                        <strong>{inspectResult.server_info.name}</strong> (版本: {inspectResult.server_info.version})
                      </div>
                    </div>
                  )}

                  {/* Summary pills */}
                  <div style={{ display: 'flex', gap: 10, marginBottom: 16 }}>
                    <span className="tag" style={{ fontSize: 12, padding: '3px 10px', color: 'var(--accent)', background: 'var(--accent-light)' }}>
                      🛠️ {inspectResult.tools?.length ?? 0} 个工具
                    </span>
                    <span className="tag" style={{ fontSize: 12, padding: '3px 10px', color: '#0284c7', background: 'rgba(2, 132, 199, 0.1)' }}>
                      📁 {inspectResult.resources?.length ?? 0} 个资源
                    </span>
                    <span className="tag" style={{ fontSize: 12, padding: '3px 10px', color: '#8b5cf6', background: 'rgba(139, 92, 246, 0.1)' }}>
                      💬 {inspectResult.prompts?.length ?? 0} 个提示词
                    </span>
                  </div>

                  {/* Tools List */}
                  {inspectResult.tools && inspectResult.tools.length > 0 && (
                    <div style={{ marginBottom: 16 }}>
                      <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-3)', textTransform: 'uppercase', marginBottom: 8 }}>
                        工具清单 (Tools)
                      </div>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                        {inspectResult.tools.map(t => (
                          <div
                            key={t.name}
                            style={{
                              background: 'var(--bg)',
                              border: '1px solid var(--border)',
                              borderRadius: 6,
                              padding: '8px 12px',
                            }}
                          >
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                              <code style={{ fontFamily: 'var(--font-mono)', fontWeight: 600, color: 'var(--text)' }}>{t.name}</code>
                            </div>
                            {t.description && (
                              <div style={{ fontSize: 12, color: 'var(--text-2)', marginTop: 4 }}>
                                {t.description}
                              </div>
                            )}
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Resources List */}
                  {inspectResult.resources && inspectResult.resources.length > 0 && (
                    <div style={{ marginBottom: 16 }}>
                      <div style={{ fontSize: 12, fontWeight: 600, color: 'var(--text-3)', textTransform: 'uppercase', marginBottom: 8 }}>
                        资源清单 (Resources)
                      </div>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                        {inspectResult.resources.map(r => (
                          <div
                            key={r.uri}
                            style={{
                              background: 'var(--bg)',
                              border: '1px solid var(--border)',
                              borderRadius: 6,
                              padding: '8px 12px',
                            }}
                          >
                            <div style={{ fontWeight: 600, fontSize: 13 }}>{r.name}</div>
                            <code style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--text-3)' }}>{r.uri}</code>
                            {r.description && <div style={{ fontSize: 12, color: 'var(--text-2)', marginTop: 2 }}>{r.description}</div>}
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Warnings if any */}
                  {inspectResult.warnings && inspectResult.warnings.length > 0 && (
                    <div className="alert warn" style={{ marginTop: 12 }}>
                      {inspectResult.warnings.join('; ')}
                    </div>
                  )}
                </div>
              )}
            </div>

            <div style={{ padding: '10px 18px', borderTop: '1px solid var(--border)', display: 'flex', justifyContent: 'flex-end' }}>
              <button className="btn" onClick={() => setInspectingProvider(null)}>
                关闭
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
