import { useEffect, useState, useMemo } from 'react'
import {
  getMcpCombos,
  getMcpComboTools,
  testMcpComboCall,
} from '../api/client'
import type { McpComboView, McpToolInfo } from '../api/client'

function IconPlay() {
  return (
    <svg width="13" height="13" viewBox="0 0 16 16" fill="currentColor">
      <polygon points="4 2 13 8 4 14" />
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

// Generate default JSON args template from JSON Schema
function generateDefaultArgs(schema?: McpToolInfo['inputSchema']): string {
  if (!schema || !schema.properties || Object.keys(schema.properties).length === 0) {
    return '{}'
  }
  const obj: Record<string, any> = {}
  for (const [key, prop] of Object.entries(schema.properties)) {
    const p = prop as any
    if (p.type === 'string') {
      obj[key] = p.default !== undefined ? p.default : ''
    } else if (p.type === 'number' || p.type === 'integer') {
      obj[key] = p.default !== undefined ? p.default : 0
    } else if (p.type === 'boolean') {
      obj[key] = p.default !== undefined ? p.default : false
    } else if (p.type === 'array') {
      obj[key] = []
    } else if (p.type === 'object') {
      obj[key] = {}
    } else {
      obj[key] = null
    }
  }
  return JSON.stringify(obj, null, 2)
}

export default function McpTest() {
  const [combos, setCombos] = useState<McpComboView[]>([])
  const [selectedCombo, setSelectedCombo] = useState('')
  const [userId, setUserId] = useState('test-user')

  const [tools, setTools] = useState<McpToolInfo[]>([])
  const [loadingTools, setLoadingTools] = useState(false)
  const [toolsError, setToolsError] = useState('')

  const [selectedToolName, setSelectedToolName] = useState('')
  const [toolSearch, setToolSearch] = useState('')

  const [argsText, setArgsText] = useState('{}')
  const [calling, setCalling] = useState(false)
  const [callResult, setCallResult] = useState<{
    success: boolean
    duration_ms: number
    result?: any
    error?: any
  } | null>(null)

  // 1. Initial load of combos
  useEffect(() => {
    getMcpCombos().then(res => {
      const list = res.combos || []
      setCombos(list)
      if (list.length > 0) {
        setSelectedCombo(list[0].name)
      }
    }).catch(err => {
      setToolsError('加载 Combos 失败: ' + err.message)
    })
  }, [])

  // 2. When combo changes, load tools with schemas
  const fetchTools = async (comboName: string) => {
    if (!comboName) return
    setLoadingTools(true)
    setToolsError('')
    try {
      const res = await getMcpComboTools(comboName)
      const list = res.tools || []
      setTools(list)
      if (list.length > 0) {
        setSelectedToolName(list[0].name)
        setArgsText(generateDefaultArgs(list[0].inputSchema))
      } else {
        setSelectedToolName('')
        setArgsText('{}')
      }
      setCallResult(null)
    } catch (err: any) {
      setToolsError(err.message || '获取工具列表失败')
      setTools([])
    } finally {
      setLoadingTools(false)
    }
  }

  useEffect(() => {
    if (selectedCombo) {
      fetchTools(selectedCombo)
    }
  }, [selectedCombo])

  // Current selected tool
  const currentTool = useMemo(() => {
    return tools.find(t => t.name === selectedToolName)
  }, [tools, selectedToolName])

  // When selected tool changes, reset args template
  const handleSelectTool = (t: McpToolInfo) => {
    setSelectedToolName(t.name)
    setArgsText(generateDefaultArgs(t.inputSchema))
    setCallResult(null)
  }

  // Filter tools
  const filteredTools = useMemo(() => {
    const q = toolSearch.toLowerCase().trim()
    return tools.filter(t => !q || t.name.toLowerCase().includes(q) || (t.description || '').toLowerCase().includes(q))
  }, [tools, toolSearch])

  // Call tool
  const handleExecute = async () => {
    if (!selectedCombo || !selectedToolName) return
    let parsedArgs = {}
    try {
      parsedArgs = JSON.parse(argsText || '{}')
    } catch (e: any) {
      alert('参数 JSON 格式无效: ' + e.message)
      return
    }

    setCalling(true)
    setCallResult(null)
    try {
      const res = await testMcpComboCall({
        combo: selectedCombo,
        tool: selectedToolName,
        arguments: parsedArgs,
        user_id: userId.trim(),
      })
      setCallResult(res)
    } catch (err: any) {
      setCallResult({
        success: false,
        duration_ms: 0,
        error: { message: err.message || '调用失败' },
      })
    } finally {
      setCalling(false)
    }
  }

  // Check if error is CodeAuthRequired (-32001)
  const isAuthRequired = callResult?.error?.code === -32001
  const authUrl = callResult?.error?.data?.auth_url
  const authProvider = callResult?.error?.data?.provider

  return (
    <div className="page" style={{ display: 'flex', flexDirection: 'column', height: '100%', paddingBottom: 60 }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 16, paddingBottom: 16, borderBottom: '1px solid var(--border)' }}>
        <div>
          <span style={{ fontSize: 20, fontWeight: 600, letterSpacing: '-0.01em', marginRight: 12 }}>MCP 工具调试器</span>
          <span style={{ fontSize: 13, color: 'var(--text-2)' }}>交互式测试 MCP Combo 聚合工具调用与多用户 OAuth2 授权验证</span>
        </div>
      </div>

      {/* Control Bar: Combo select + User ID */}
      <div style={{
        display: 'flex',
        alignItems: 'center',
        gap: 16,
        background: 'var(--bg-panel)',
        border: '1px solid var(--border)',
        borderRadius: 8,
        padding: '12px 16px',
        marginBottom: 16,
        boxShadow: '0 1px 3px rgba(0,0,0,0.05)',
      }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 13, fontWeight: 600 }}>目标 Combo:</span>
          <select
            value={selectedCombo}
            onChange={e => setSelectedCombo(e.target.value)}
            style={{ minWidth: 180, fontWeight: 500 }}
          >
            {combos.map(c => (
              <option key={c.name} value={c.name}>
                {c.name} ({c.tool_count} 个工具)
              </option>
            ))}
          </select>
          <button
            className="btn-icon"
            title="重新拉取工具列表"
            onClick={() => fetchTools(selectedCombo)}
            disabled={loadingTools}
          >
            <IconRefresh />
          </button>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ fontSize: 13, fontWeight: 600 }}>测试用户 (X-User-Id):</span>
          <input
            value={userId}
            placeholder="alice"
            onChange={e => setUserId(e.target.value)}
            style={{ width: 140, fontFamily: 'var(--font-mono)', fontSize: 12 }}
          />
          <span style={{ fontSize: 11, color: 'var(--text-3)' }}>（测试隔离认证时模拟不同用户）</span>
        </div>
      </div>

      {toolsError && <div className="alert err" style={{ marginBottom: 16 }}>{toolsError}</div>}

      {/* Main Two-Column Playground */}
      <div style={{
        display: 'flex',
        flex: 1,
        background: 'var(--bg-panel)',
        border: '1px solid var(--border)',
        borderRadius: 10,
        overflow: 'hidden',
        boxShadow: '0 1px 3px rgba(0,0,0,0.08)',
      }}>
        {/* Left Column: Tool Explorer */}
        <div style={{
          width: 320,
          minWidth: 320,
          borderRight: '1px solid var(--border)',
          display: 'flex',
          flexDirection: 'column',
          background: 'var(--bg)',
        }}>
          <div style={{ padding: '12px 12px 8px', borderBottom: '1px solid var(--border)' }}>
            <input
              value={toolSearch}
              onChange={e => setToolSearch(e.target.value)}
              placeholder="搜索工具名称或描述..."
              style={{ fontSize: 12, width: '100%' }}
            />
            <div style={{ fontSize: 11, color: 'var(--text-3)', marginTop: 6, display: 'flex', justifyContent: 'space-between' }}>
              <span>共 {tools.length} 个聚合工具</span>
              {loadingTools && <span>加载中...</span>}
            </div>
          </div>

          <div style={{ flex: 1, overflowY: 'auto', padding: '6px 8px' }}>
            {filteredTools.length === 0 ? (
              <div style={{ padding: 24, fontSize: 12, color: 'var(--text-3)', textAlign: 'center' }}>
                {loadingTools ? '正在拉取工具列表...' : '未找到匹配工具'}
              </div>
            ) : (
              filteredTools.map(t => {
                const isSelected = t.name === selectedToolName
                return (
                  <div
                    key={t.name}
                    onClick={() => handleSelectTool(t)}
                    style={{
                      padding: '10px 12px',
                      marginBottom: 4,
                      borderRadius: 6,
                      cursor: 'pointer',
                      background: isSelected ? 'var(--bg-panel)' : 'transparent',
                      border: isSelected ? '1px solid var(--border)' : '1px solid transparent',
                      boxShadow: isSelected ? '0 1px 3px rgba(0,0,0,0.06)' : 'none',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginBottom: 2 }}>
                      <span style={{ fontWeight: isSelected ? 600 : 500, fontSize: 13, fontFamily: 'var(--font-mono)', color: isSelected ? 'var(--accent)' : 'var(--text)' }}>
                        {t.name}
                      </span>
                    </div>
                    {t.description && (
                      <div style={{
                        fontSize: 11,
                        color: 'var(--text-3)',
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
                )
              })
            )}
          </div>
        </div>

        {/* Right Column: Execution & Results */}
        <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflowY: 'auto' }}>
          {currentTool ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 20, padding: 24 }}>
              {/* Tool Header Info */}
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                  <h2 style={{ fontSize: 18, fontWeight: 600, fontFamily: 'var(--font-mono)', margin: 0 }}>
                    {currentTool.name}
                  </h2>
                  <span className="badge badge-ok">MCP Tool</span>
                </div>
                {currentTool.description && (
                  <p style={{ margin: '8px 0 0 0', color: 'var(--text-2)', fontSize: 13, lineHeight: 1.5 }}>
                    {currentTool.description}
                  </p>
                )}
              </div>

              {/* Schema Parameters Documentation */}
              {currentTool.inputSchema?.properties && Object.keys(currentTool.inputSchema.properties).length > 0 && (
                <div style={{ background: 'var(--bg)', border: '1px solid var(--border)', borderRadius: 6, padding: '12px 16px' }}>
                  <div style={{ fontSize: 11, fontWeight: 700, textTransform: 'uppercase', color: 'var(--text-3)', marginBottom: 8 }}>
                    参数说明 (Schema Properties)
                  </div>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                    {Object.entries(currentTool.inputSchema.properties).map(([pName, pProp]) => {
                      const prop = pProp as any
                      const isRequired = (currentTool.inputSchema?.required || []).includes(pName)
                      return (
                        <div key={pName} style={{ display: 'grid', gridTemplateColumns: '160px 80px 1fr', gap: 8, fontSize: 12, alignItems: 'baseline' }}>
                          <div>
                            <code style={{ fontFamily: 'var(--font-mono)', fontWeight: 600, color: 'var(--accent)' }}>{pName}</code>
                            {isRequired && <span style={{ color: 'var(--err-fg)', marginLeft: 4 }}>*</span>}
                          </div>
                          <span style={{ color: 'var(--text-3)' }}>{prop.type || 'any'}</span>
                          <span style={{ color: 'var(--text-2)' }}>{prop.description || '—'}</span>
                        </div>
                      )
                    })}
                  </div>
                </div>
              )}

              {/* JSON Arguments Editor */}
              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
                  <label style={{ fontSize: 12, fontWeight: 600, color: 'var(--text)' }}>
                    输入参数 (JSON Arguments):
                  </label>
                  <button
                    className="btn"
                    style={{ fontSize: 11, padding: '2px 8px' }}
                    onClick={() => setArgsText(generateDefaultArgs(currentTool.inputSchema))}
                  >
                    重置为 Schema 模板
                  </button>
                </div>
                <textarea
                  value={argsText}
                  onChange={e => setArgsText(e.target.value)}
                  rows={7}
                  spellCheck={false}
                  style={{
                    width: '100%',
                    fontFamily: 'var(--font-mono)',
                    fontSize: 13,
                    padding: 12,
                    background: 'var(--bg)',
                    border: '1px solid var(--border-md)',
                    borderRadius: 6,
                    color: 'var(--text)',
                    boxSizing: 'border-box',
                    outline: 'none',
                    resize: 'vertical',
                  }}
                />
              </div>

              {/* Action Button */}
              <div>
                <button
                  className="btn-primary"
                  onClick={handleExecute}
                  disabled={calling}
                  style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '8px 20px', fontSize: 13 }}
                >
                  <IconPlay />
                  {calling ? '正在执行...' : '执行调用 (tools/call)'}
                </button>
              </div>

              {/* Result Console */}
              {callResult && (
                <div style={{
                  background: 'var(--bg)',
                  border: '1px solid var(--border)',
                  borderRadius: 8,
                  padding: 16,
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 12,
                }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', borderBottom: '1px solid var(--border)', paddingBottom: 8 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <span style={{ fontWeight: 600, fontSize: 13 }}>调用结果</span>
                      {callResult.success ? (
                        <span className="badge badge-ok">200 OK</span>
                      ) : (
                        <span className="badge" style={{ background: 'var(--err-bg)', color: 'var(--err-fg)' }}>
                          {isAuthRequired ? '401 Auth Required' : 'Error'}
                        </span>
                      )}
                    </div>
                    <span style={{ fontSize: 11, color: 'var(--text-3)' }}>
                      耗时: {callResult.duration_ms} ms
                    </span>
                  </div>

                  {/* Auth Required (-32001) Special Display */}
                  {isAuthRequired && (
                    <div style={{
                      background: 'var(--warn-bg)',
                      border: '1px solid #facc15',
                      borderRadius: 6,
                      padding: 14,
                    }}>
                      <div style={{ color: 'var(--warn-fg)', fontWeight: 600, fontSize: 13, marginBottom: 4 }}>
                        ⚠️ 该工具需要用户授权认证 (Code: -32001)
                      </div>
                      <div style={{ fontSize: 12, color: 'var(--text-2)', marginBottom: 12 }}>
                        用户 <strong>{userId}</strong> 尚未绑定提供商 <strong>{authProvider}</strong> 的账号。请点击下方按钮完成 OAuth 2.1 授权。
                      </div>
                      {authUrl && (
                        <div style={{ display: 'flex', gap: 10, alignItems: 'center' }}>
                          <a
                            href={authUrl}
                            target="_blank"
                            rel="noreferrer"
                            className="btn-primary"
                            style={{ display: 'inline-flex', alignItems: 'center', gap: 6, textDecoration: 'none', fontSize: 12 }}
                          >
                            <IconExternal /> 前往授权登录
                          </a>
                          <button
                            className="btn"
                            onClick={handleExecute}
                            style={{ fontSize: 12 }}
                          >
                            授权完成后重新调用
                          </button>
                        </div>
                      )}
                    </div>
                  )}

                  {/* General Error */}
                  {!callResult.success && !isAuthRequired && (
                    <div style={{ color: 'var(--err-fg)', fontSize: 13, fontFamily: 'var(--font-mono)' }}>
                      {callResult.error ? JSON.stringify(callResult.error, null, 2) : '未知错误'}
                    </div>
                  )}

                  {/* Success Result Display */}
                  {callResult.success && callResult.result && (
                    <div>
                      {callResult.result.isError && (
                        <div className="alert err" style={{ marginBottom: 8 }}>
                          工具返回执行错误标志 (isError: true)
                        </div>
                      )}

                      {callResult.result.content && Array.isArray(callResult.result.content) ? (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                          {callResult.result.content.map((item: any, cIdx: number) => (
                            <div key={cIdx} style={{ background: 'var(--bg-panel)', border: '1px solid var(--border)', borderRadius: 6, padding: 12 }}>
                              <span className="badge" style={{ fontSize: 10, marginBottom: 6 }}>{item.type}</span>
                              {item.type === 'text' && (
                                <pre style={{ margin: 0, fontFamily: 'var(--font-mono)', fontSize: 12, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>
                                  {item.text}
                                </pre>
                              )}
                              {item.type === 'image' && (
                                <img
                                  src={`data:${item.mimeType || 'image/png'};base64,${item.data}`}
                                  alt="tool result"
                                  style={{ maxWidth: '100%', borderRadius: 4 }}
                                />
                              )}
                            </div>
                          ))}
                        </div>
                      ) : (
                        <pre style={{ margin: 0, fontFamily: 'var(--font-mono)', fontSize: 12, whiteSpace: 'pre-wrap' }}>
                          {JSON.stringify(callResult.result, null, 2)}
                        </pre>
                      )}
                    </div>
                  )}
                </div>
              )}
            </div>
          ) : (
            <div style={{ padding: 64, textAlign: 'center', color: 'var(--text-3)' }}>
              请在左侧选择一个工具进行参数配置与调用测试
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
