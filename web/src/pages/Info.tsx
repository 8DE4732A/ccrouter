import { useEffect, useRef, useState } from 'react'
import { getAdminInfo, getKeysStatus, FMT_COLOR } from '../api/client'
import type { AdminInfo, KeysStatusResp } from '../api/client'

export default function InfoPage() {
  const [info, setInfo] = useState<AdminInfo | null>(null)
  const [keys, setKeys] = useState<KeysStatusResp | null>(null)
  const [err, setErr] = useState('')
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const loadKeys = () => getKeysStatus().then(setKeys).catch(() => {})

  useEffect(() => {
    getAdminInfo().then(setInfo).catch(e => setErr(String(e)))
    loadKeys()
    // Refresh key status every 10 s so cooldown countdowns stay current
    timerRef.current = setInterval(loadKeys, 10_000)
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [])

  if (!info) return (
    <div className="page">
      {err
        ? <div className="alert err">{err}</div>
        : <div className="empty-state">加载中…</div>}
    </div>
  )

  return (
    <div className="page">
      <div className="page-header">
        <h1 className="page-title">信息</h1>
      </div>

      {/* Version card */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 12, marginBottom: 28 }}>
        <div className="stat-card">
          <div className="stat-label">应用版本</div>
          <div className="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: 20 }}>
            v{info.version}
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-label">运行时</div>
          <div className="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: 20 }}>
            {info.runtime}
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Combo 数</div>
          <div className="stat-value">{info.combos.length}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Provider 数</div>
          <div className="stat-value">{info.providers.length}</div>
        </div>
      </div>

      {/* Combos */}
      <section style={{ marginBottom: 28 }}>
        <h2 style={{ fontSize: 13, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.07em', color: 'var(--text-3)', marginBottom: 12 }}>
          可用模型（Combo）
        </h2>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {info.combos.map(c => (
            <div key={c.name} style={{
              background: 'var(--bg-panel)',
              border: '1px solid var(--border)',
              borderRadius: 8,
              padding: '14px 18px',
            }}>
              <div style={{ display: 'flex', alignItems: 'flex-start', gap: 12, flexWrap: 'wrap' }}>
                {/* Name + aliases */}
                <div style={{ flex: 1, minWidth: 180 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', marginBottom: 4 }}>
                    <code style={{
                      fontFamily: 'var(--font-mono)', fontSize: 14, fontWeight: 600,
                      color: 'var(--accent)', background: 'var(--accent-light)',
                      padding: '2px 8px', borderRadius: 4,
                    }}>{c.name}</code>
                    {(c.aliases ?? []).map(a => (
                      <code key={a} style={{
                        fontFamily: 'var(--font-mono)', fontSize: 12,
                        color: 'var(--text-2)', background: 'var(--bg)',
                        border: '1px solid var(--border-md)',
                        padding: '1px 7px', borderRadius: 4,
                      }}>{a}</code>
                    ))}
                  </div>
                  <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                    {c.api_formats.map(f => (
                      <span key={f} className={`tag ${(FMT_COLOR as Record<string, string>)[f] ?? ''}`} style={{ fontSize: 10 }}>{f}</span>
                    ))}
                    <span className="tag" style={{ fontSize: 10 }}>{c.strategy}</span>
                  </div>
                </div>

                {/* Members */}
                <div style={{ display: 'flex', flexDirection: 'column', gap: 4, minWidth: 240 }}>
                  {c.members.map((m, i) => (
                    <div key={i} style={{
                      display: 'flex', alignItems: 'center', gap: 8,
                      fontSize: 12, color: 'var(--text-2)',
                    }}>
                      <span style={{
                        width: 18, height: 18, borderRadius: '50%',
                        background: 'var(--bg)', border: '1px solid var(--border-md)',
                        display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                        fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--text-3)',
                        flexShrink: 0,
                      }}>{i + 1}</span>
                      <span style={{ color: 'var(--text-3)' }}>{m.provider}</span>
                      <span style={{ color: 'var(--border-md)' }}>›</span>
                      <code style={{ fontFamily: 'var(--font-mono)', color: 'var(--text)' }}>{m.model}</code>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* Providers */}
      <section style={{ marginBottom: 28 }}>
        <h2 style={{ fontSize: 13, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.07em', color: 'var(--text-3)', marginBottom: 12 }}>
          Providers
        </h2>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(240px, 1fr))', gap: 8 }}>
          {info.providers.map(p => (
            <div key={p.name} style={{
              background: 'var(--bg-panel)',
              border: '1px solid var(--border)',
              borderRadius: 8,
              padding: '14px 18px',
            }}>
              <div style={{ fontWeight: 600, fontSize: 14, marginBottom: 8 }}>{p.name}</div>
              <div style={{ display: 'flex', gap: 4, flexWrap: 'wrap', marginBottom: 8 }}>
                {p.api_formats.map(f => (
                  <span key={f} className={`tag ${(FMT_COLOR as Record<string, string>)[f] ?? ''}`} style={{ fontSize: 10 }}>{f}</span>
                ))}
              </div>
              <div style={{ fontSize: 12, color: 'var(--text-3)', display: 'flex', gap: 16 }}>
                <span>{p.key_count} 个密钥</span>
                <span>{p.strategy}</span>
              </div>
            </div>
          ))}
        </div>
      </section>

      {/* Key status */}
      {keys && keys.providers.length > 0 && (
        <section>
          <h2 style={{ fontSize: 13, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.07em', color: 'var(--text-3)', marginBottom: 12 }}>
            Key 状态 <span style={{ fontSize: 11, fontWeight: 400, textTransform: 'none', letterSpacing: 0, color: 'var(--text-3)', marginLeft: 6 }}>每 10 秒自动刷新</span>
          </h2>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
            {keys.providers.map(prov => (
              <div key={prov.provider} style={{
                background: 'var(--bg-panel)',
                border: '1px solid var(--border)',
                borderRadius: 8,
                overflow: 'hidden',
              }}>
                {/* Provider header */}
                <div style={{
                  padding: '10px 16px',
                  borderBottom: '1px solid var(--border)',
                  display: 'flex', alignItems: 'center', gap: 10,
                  background: 'var(--bg)',
                }}>
                  <span style={{ fontWeight: 600, fontSize: 13 }}>{prov.provider}</span>
                  <span className="tag" style={{ fontSize: 10 }}>{prov.strategy}</span>
                  <span style={{ marginLeft: 'auto', fontSize: 12, color: 'var(--text-3)' }}>
                    {prov.keys.filter(k => Object.values(k.model_cooldowns).every(cd => cd.available !== false)).length}
                    {' / '}{prov.keys.length} 可用
                  </span>
                </div>
                {/* Key rows */}
                <div>
                  {prov.keys.map((k, ki) => {
                    const coolingModels = Object.entries(k.model_cooldowns).filter(([, cd]) => cd.available === false)
                    const isCooling = coolingModels.length > 0
                    return (
                      <div key={ki} style={{
                        padding: '10px 16px',
                        borderBottom: ki < prov.keys.length - 1 ? '1px solid var(--border)' : undefined,
                        display: 'flex', alignItems: 'flex-start', gap: 12,
                        background: isCooling ? 'var(--err-bg, rgba(239,68,68,0.04))' : undefined,
                      }}>
                        {/* Index */}
                        <span style={{
                          width: 20, height: 20, borderRadius: '50%', flexShrink: 0,
                          background: isCooling ? 'var(--err-fg)' : 'var(--ok-fg)',
                          display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                          color: '#fff', fontSize: 10, fontWeight: 700, marginTop: 1,
                        }}>{ki + 1}</span>
                        {/* Key prefix */}
                        <code style={{
                          fontFamily: 'var(--font-mono)', fontSize: 12,
                          color: isCooling ? 'var(--err-fg)' : 'var(--text)',
                          minWidth: 80,
                        }}>{k.key_prefix}…</code>
                        {/* Stats */}
                        <div style={{ display: 'flex', gap: 16, fontSize: 11, color: 'var(--text-3)', flexShrink: 0 }}>
                          <span title="成功次数">✓ {k.use_count}</span>
                          <span title="错误次数" style={k.error_count > 0 ? { color: 'var(--err-fg)' } : undefined}>✗ {k.error_count}</span>
                        </div>
                        {/* Cooldown detail */}
                        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginLeft: 'auto' }}>
                          {coolingModels.length === 0 ? (
                            <span style={{ fontSize: 11, color: 'var(--ok-fg)', fontWeight: 500 }}>正常</span>
                          ) : coolingModels.map(([model, cd]) => (
                            <span key={model} style={{
                              fontSize: 11, padding: '2px 8px', borderRadius: 4,
                              background: 'var(--err-bg, rgba(239,68,68,0.1))',
                              color: 'var(--err-fg)',
                              fontFamily: 'var(--font-mono)',
                            }}>
                              {model} 冷却 {cd.seconds_remaining != null ? `${Math.ceil(cd.seconds_remaining)}s` : ''}
                            </span>
                          ))}
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  )
}
