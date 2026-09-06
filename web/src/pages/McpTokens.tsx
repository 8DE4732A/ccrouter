import { useEffect, useState } from 'react'
import {
  getMcpTokens,
  deleteMcpToken,
  getMcpProviders,
} from '../api/client'
import type { McpTokenItem, McpProviderConfig } from '../api/client'

function IconTrash() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8">
      <polyline points="3 4 13 4" />
      <path d="M5 4V3a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v1" />
      <path d="M6 7v5M10 7v5M4 4l1 9h6l1-9" />
    </svg>
  )
}

function IconRefresh() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8">
      <path d="M2.5 8a5.5 5.5 0 0 1 9.3-3.9L14 6.5M14 2v4.5H9.5" />
      <path d="M13.5 8a5.5 5.5 0 0 1-9.3 3.9L2 9.5M2 14v-4.5h4.5" />
    </svg>
  )
}

function IconExternal() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.8">
      <path d="M12 9v4a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1h4" />
      <polyline points="10 2 14 2 14 6" />
      <line x1="7" y1="9" x2="14" y2="2" />
    </svg>
  )
}

export default function McpTokens() {
  const [tokens, setTokens] = useState<McpTokenItem[]>([])
  const [providers, setProviders] = useState<McpProviderConfig[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // Quick Auth Link Generator
  const [genProvider, setGenProvider] = useState('')
  const [genUserId, setGenUserId] = useState('')
  const [generatedUrl, setGeneratedUrl] = useState('')

  const loadData = async () => {
    setLoading(true)
    setError('')
    try {
      const [tokRes, provRes] = await Promise.all([
        getMcpTokens().catch(() => ({ tokens: [] })),
        getMcpProviders().catch(() => ({ providers: [] })),
      ])
      setTokens(tokRes.tokens || [])
      const pList = (provRes.providers || []).filter(p => p.auth?.mode === 'oauth2' || p.auth_mode === 'isolated')
      setProviders(pList)
      if (pList.length > 0 && !genProvider) {
        setGenProvider(pList[0].name)
      }
    } catch (err: any) {
      setError(err.message || '加载凭据列表失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  const handleDelete = async (id: number, prov: string, uid: string) => {
    if (!confirm(`确认撤销用户 "${uid}" 对提供商 "${prov}" 的授权凭据？\n撤销后该用户下次调用时需重新进行 OAuth 授权。`)) {
      return
    }
    try {
      await deleteMcpToken(id)
      setTokens(prev => prev.filter(t => t.id !== id))
    } catch (err: any) {
      alert('撤销失败: ' + err.message)
    }
  }

  const handleGenerateUrl = () => {
    if (!genProvider || !genUserId.trim()) {
      alert('请选择 Provider 并输入 User ID')
      return
    }
    const origin = window.location.origin
    const url = `${origin}/mcp/auth/start?provider=${encodeURIComponent(genProvider)}&user_id=${encodeURIComponent(genUserId.trim())}`
    setGeneratedUrl(url)
  }

  return (
    <div className="page" style={{ display: 'flex', flexDirection: 'column', gap: 20, paddingBottom: 60 }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', paddingBottom: 16, borderBottom: '1px solid var(--border)' }}>
        <div>
          <span style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.01em', marginRight: 12 }}>用户授权凭据</span>
          <span style={{ fontSize: 13, color: 'var(--text-2)' }}>管理下游各用户独立持有的 OAuth 2.1 Access Tokens 与授权状态</span>
        </div>
        <button className="btn" onClick={loadData} disabled={loading} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <IconRefresh />
          {loading ? '刷新中...' : '刷新'}
        </button>
      </div>

      {error && <div className="alert err">{error}</div>}

      {/* Quick Auth URL Generator Card */}
      <div style={{
        background: 'var(--bg-panel)',
        border: '1px solid var(--border)',
        borderRadius: 8,
        padding: '16px 20px',
        boxShadow: '0 1px 3px rgba(0,0,0,0.05)',
      }}>
        <div style={{ fontWeight: 600, fontSize: 13, marginBottom: 4 }}>
          快捷生成 OAuth 授权测试链接
        </div>
        <div style={{ fontSize: 12, color: 'var(--text-3)', marginBottom: 12 }}>
          模拟聊天机器人向下游指定用户派发授权链接，点击在新标签页完成 OAuth 握手回跳
        </div>
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{ fontSize: 12, color: 'var(--text-2)' }}>Provider:</span>
            <select
              value={genProvider}
              onChange={e => setGenProvider(e.target.value)}
              style={{ minWidth: 160 }}
            >
              {providers.length === 0 && <option value="">无 OAuth Provider</option>}
              {providers.map(p => (
                <option key={p.name} value={p.name}>
                  {p.name}
                </option>
              ))}
            </select>
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{ fontSize: 12, color: 'var(--text-2)' }}>User ID:</span>
            <input
              value={genUserId}
              placeholder="例如 alice, user-101"
              onChange={e => setGenUserId(e.target.value)}
              style={{ width: 160, fontFamily: 'var(--font-mono)' }}
            />
          </div>

          <button className="btn-primary" onClick={handleGenerateUrl} style={{ fontSize: 12 }}>
            生成链接
          </button>
        </div>

        {generatedUrl && (
          <div style={{
            marginTop: 12,
            padding: '10px 14px',
            background: 'var(--bg)',
            borderRadius: 6,
            border: '1px solid var(--border)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 12,
          }}>
            <code style={{ fontFamily: 'var(--font-mono)', fontSize: 12, wordBreak: 'break-all', color: 'var(--accent)' }}>
              {generatedUrl}
            </code>
            <div style={{ display: 'flex', gap: 8, flexShrink: 0 }}>
              <button
                className="btn"
                style={{ fontSize: 11 }}
                onClick={() => {
                  navigator.clipboard.writeText(generatedUrl)
                  alert('已复制授权链接到剪贴板')
                }}
              >
                复制
              </button>
              <a
                href={generatedUrl}
                target="_blank"
                rel="noreferrer"
                className="btn-primary"
                style={{ display: 'inline-flex', alignItems: 'center', gap: 4, textDecoration: 'none', fontSize: 11 }}
              >
                <IconExternal /> 打开授权
              </a>
            </div>
          </div>
        )}
      </div>

      {/* Tokens Table Card */}
      <div style={{
        background: 'var(--bg-panel)',
        border: '1px solid var(--border)',
        borderRadius: 8,
        overflow: 'hidden',
        boxShadow: '0 1px 3px rgba(0,0,0,0.05)',
      }}>
        <div style={{ padding: '12px 18px', borderBottom: '1px solid var(--border)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span style={{ fontWeight: 600, fontSize: 13 }}>已保存凭据列表</span>
          <span style={{ fontSize: 12, color: 'var(--text-3)' }}>共 {tokens.length} 条记录</span>
        </div>

        {tokens.length === 0 ? (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-3)', fontSize: 13 }}>
            暂无已授权的用户凭据。当下游用户完成 OAuth 授权后，凭据将自动持久化在此处。
          </div>
        ) : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
            <thead>
              <tr style={{ background: 'var(--bg)', borderBottom: '1px solid var(--border)', textAlign: 'left' }}>
                <th style={{ padding: '10px 14px', fontWeight: 600, color: 'var(--text-2)' }}>Provider</th>
                <th style={{ padding: '10px 14px', fontWeight: 600, color: 'var(--text-2)' }}>User ID</th>
                <th style={{ padding: '10px 14px', fontWeight: 600, color: 'var(--text-2)' }}>Access Token</th>
                <th style={{ padding: '10px 14px', fontWeight: 600, color: 'var(--text-2)' }}>类型</th>
                <th style={{ padding: '10px 14px', fontWeight: 600, color: 'var(--text-2)' }}>Scopes</th>
                <th style={{ padding: '10px 14px', fontWeight: 600, color: 'var(--text-2)' }}>更新时间</th>
                <th style={{ padding: '10px 14px', fontWeight: 600, color: 'var(--text-2)', textAlign: 'right' }}>操作</th>
              </tr>
            </thead>
            <tbody>
              {tokens.map(t => (
                <tr key={t.id} style={{ borderBottom: '1px solid var(--border)' }}>
                  <td style={{ padding: '12px 14px', fontWeight: 600 }}>
                    <span className="badge badge-ok">{t.provider}</span>
                  </td>
                  <td style={{ padding: '12px 14px', fontFamily: 'var(--font-mono)' }}>
                    {t.user_id}
                  </td>
                  <td style={{ padding: '12px 14px', fontFamily: 'var(--font-mono)', color: 'var(--text-2)' }}>
                    {t.masked_key}
                  </td>
                  <td style={{ padding: '12px 14px', color: 'var(--text-3)' }}>
                    {t.token_type || 'Bearer'}
                  </td>
                  <td style={{ padding: '12px 14px', fontSize: 12, color: 'var(--text-2)' }}>
                    {t.scopes || '—'}
                  </td>
                  <td style={{ padding: '12px 14px', fontSize: 12, color: 'var(--text-3)' }}>
                    {t.updated_at ? new Date(t.updated_at * 1000).toLocaleString() : '—'}
                  </td>
                  <td style={{ padding: '12px 14px', textAlign: 'right' }}>
                    <button
                      className="btn-icon"
                      title="撤销该凭据"
                      onClick={() => handleDelete(t.id, t.provider, t.user_id)}
                      style={{ color: 'var(--err-fg)' }}
                    >
                      <IconTrash />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
