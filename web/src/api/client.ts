/** Thin fetch wrapper with baseURL /admin/api */

const BASE = '/admin/api'
const TOKEN_KEY = 'ccrouter_admin_token'

export function getAdminToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setAdminToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearAdminToken() {
  localStorage.removeItem(TOKEN_KEY)
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const token = getAdminToken()
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...((init?.headers as Record<string, string>) || {}),
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const resp = await fetch(`${BASE}${path}`, {
    ...init,
    headers,
  })
  if (!resp.ok) {
    if (resp.status === 401) {
      clearAdminToken()
      window.dispatchEvent(new CustomEvent('ccrouter:unauthorized'))
    }
    const err = await resp.json().catch(() => ({ error: resp.statusText }))
    throw new Error(err.error ?? resp.statusText)
  }
  return resp.json() as Promise<T>
}

// ---- Config ----

export type ProxyConfig = {
  url?: string
  disabled?: boolean
}

export type APIKeyEntry = {
  key: string
}

export type GeneralConfig = {
  api_keys?: APIKeyEntry[]
  proxy?: ProxyConfig
  request_timeout_seconds?: number  // upstream request timeout in seconds, default 600 (10min)
  admin_password?: string
}

export type HealthCheckRule = {
  description: string
  jsonpath: string
  match_value: string
  match_type: 'equals' | 'contains' | 'regex'
  action: string
  cooldown_seconds: number
  models: string[]
  http_status_codes?: number[]
}

export type KeyConfig = { key: string }

export type ApiEndpoint = {
  api_format: ApiFormat
  base_url: string
}

export type ProviderConfig = {
  name: string
  api: ApiEndpoint[]
  max_retries: number
  key_strategy: 'fill-first' | 'round-robin'
  keys: KeyConfig[]
  health_check_rules: HealthCheckRule[]
  proxy?: ProxyConfig
}

export type ComboMember = {
  provider: string
  model: string
  upstream_api_format?: string  // optional: override the API format sent to this upstream
}

export type ApiFormat = 'openai' | 'anthropic' | 'openai-responses' | 'openai-images' | 'openai-image-edits' | 'openai-embeddings' | 'gemini'

export const FMT_ENDPOINT: Record<ApiFormat, string> = {
  'openai': '/v1/chat/completions',
  'anthropic': '/v1/messages',
  'openai-responses': '/v1/responses',
  'openai-images': '/v1/images/generations',
  'openai-image-edits': '/v1/images/edits',
  'openai-embeddings': '/v1/embeddings',
  'gemini': '/v1beta/models',
}

export const FMT_COLOR: Record<ApiFormat, string> = {
  'openai': 'green',
  'anthropic': 'blue',
  'openai-responses': 'amber',
  'openai-images': 'amber',
  'openai-image-edits': 'orange',
  'openai-embeddings': 'cyan',
  'gemini': 'purple',
}

export function normalizeFormats(v: ApiFormat | ApiFormat[]): ApiFormat[] {
  return Array.isArray(v) ? v : [v]
}

export type ComboConfig = {
  name: string
  owned_by?: string
  default?: boolean
  is_default?: boolean
  api_format: ApiFormat | ApiFormat[]
  strategy: 'fill-first' | 'round-robin'
  members: ComboMember[]
  aliases?: string[]
}

export type PayloadScript = {
  name: string
  enabled: boolean
  script: string
}

export type LoggingConfig = {
  enabled?: boolean
  dir?: string
  max_file_size_mb?: number
  max_backups?: number
  compression_level?: 'fastest' | 'default' | 'better' | 'best' | string
}

export type AppConfig = {
  general?: GeneralConfig
  logging?: LoggingConfig
  verbose_logging?: boolean
  providers: ProviderConfig[]
  combos: ComboConfig[]
  mcp_providers?: McpProviderConfig[]
  mcp_combos?: McpComboConfig[]
  payload_scripts?: PayloadScript[]
}

export const getConfig = () => request<AppConfig>('/config')
export const putConfig = (cfg: AppConfig) =>
  request<AppConfig>('/config', { method: 'PUT', body: JSON.stringify(cfg) })

// ---- Stats ----

export type ModelCooldownEntry = { available: boolean; seconds_remaining?: number }
export type KeyStat = {
  key_prefix: string
  use_count: number
  error_count: number
  last_used_at: number | null
  model_cooldowns: Record<string, ModelCooldownEntry>
}
export type ProviderStat = { provider: string; strategy: string; keys: KeyStat[] }
export type KeysStatus = { combos: string[]; providers: ProviderStat[] }

export const getStatsKeys = () => request<KeysStatus>('/stats/keys')

export type SummaryRow = {
  group_key: string
  total: number
  success_count: number
  error_count: number
  total_tokens: number
  prompt_tokens: number
  completion_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  avg_duration_ms: number | null
}
export type SummaryResp = { data: SummaryRow[]; group_by: string }

export const getStatsSummary = (params: {
  group_by?: string
  since?: number
  until?: number
}) => {
  const qs = new URLSearchParams()
  if (params.group_by) qs.set('group_by', params.group_by)
  if (params.since != null) qs.set('since', String(params.since))
  if (params.until != null) qs.set('until', String(params.until))
  return request<SummaryResp>(`/stats/summary?${qs}`)
}

export type TrendRow = { bucket_ts: number; total: number; success_count: number; total_tokens: number }
export type TrendResp = { data: TrendRow[]; bucket: string }

export const getStatsTrend = (params: {
  bucket?: string
  since?: number
  until?: number
}) => {
  const qs = new URLSearchParams()
  if (params.bucket) qs.set('bucket', params.bucket)
  if (params.since != null) qs.set('since', String(params.since))
  if (params.until != null) qs.set('until', String(params.until))
  return request<TrendResp>(`/stats/trend?${qs}`)
}

// ---- Requests ----

export type RequestRow = {
  id: number
  ts: number
  combo: string | null
  provider: string | null
  model: string | null
  key_prefix: string | null
  api_format: string | null
  is_stream: number
  status_code: number | null
  success: number
  matched_rule: string | null
  matched_payload: string | null
  prompt_tokens: number | null
  completion_tokens: number | null
  total_tokens: number | null
  cache_read_tokens: number | null
  cache_write_tokens: number | null
  duration_ms: number | null
  error: string | null
}
export type RequestsResp = { total: number; items: RequestRow[] }

export const getRequests = (params: {
  limit?: number
  offset?: number
  combo?: string
  provider?: string
  model?: string
  success?: boolean
  since?: number
  until?: number
}) => {
  const qs = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v != null) qs.set(k, String(v))
  })
  return request<RequestsResp>(`/requests?${qs}`)
}

// ---- Info ----
export type InfoCombo = {
  name: string
  owned_by?: string
  is_default?: boolean
  full_id?: string
  aliases: string[]
  api_formats: string[]
  strategy: string
  members: { provider: string; model: string }[]
}

export type InfoProvider = {
  name: string
  api_formats: string[]
  key_count: number
  strategy: string
}
export type AdminInfo = {
  version: string
  runtime: string
  combos: InfoCombo[]
  providers: InfoProvider[]
}
export const getAdminInfo = () => request<AdminInfo>('/info')

// ---- Keys status ----
export type KeyCooldown = { available: boolean; seconds_remaining?: number }
export type KeyEntry = {
  key_prefix: string
  use_count: number
  error_count: number
  last_used_at: number | null
  model_cooldowns: Record<string, KeyCooldown>
}
export type ProviderKeysStatus = {
  provider: string
  strategy: string
  keys: KeyEntry[]
}
export type KeysStatusResp = {
  combos: string[]
  providers: ProviderKeysStatus[]
}
export const getKeysStatus = () => request<KeysStatusResp>('/stats/keys')

// ---- Health ----
export type AdminHealth = {
  status: string
  python: string
  config: { providers: number; combos: number }
  db: { queue_size: number; dropped_count: number; db_path: string }
}
export const getAdminHealth = () => request<AdminHealth>('/health')

// ---- Verbose Logs ----
type LogHttpPart = { method?: string; path?: string; url?: string; headers: Record<string, string>; body: unknown }

// List row — no request/response body, just metadata
export type LogRow = {
  ts: number
  combo: string | null
  provider: string | null
  model: string | null
  api_format: string | null
  is_stream: boolean
  status_code: number | null
  success: boolean
  duration_ms: number | null
}

// Full detail — includes request + response bodies, fetched on demand
export type LogRecord = LogRow & {
  request: { client: LogHttpPart; upstream: LogHttpPart }
  response: { status_code: number | null; headers: Record<string, string>; body: unknown }
}

export type LogsResp = { items: LogRow[]; has_more: boolean }
export type LogSettings = {
  verbose_logging: boolean
  enabled?: boolean
  dir?: string
  max_file_size_mb?: number
  max_backups?: number
  compression_level?: string
}

export type PutLogSettingsPayload = {
  enabled?: boolean
  verbose_logging?: boolean
  dir?: string
  max_file_size_mb?: number
  max_backups?: number
  compression_level?: string
}

export const getLogs = (params: { limit?: number; offset?: number; success?: boolean }) => {
  const qs = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => { if (v != null) qs.set(k, String(v)) })
  return request<LogsResp>(`/logs?${qs}`)
}
export const getLogDetail = (ts: number) => request<LogRecord>(`/logs/detail/${ts}`)
export const getLogSettings = () => request<LogSettings>('/logs/settings')
export const putLogSettings = (payload: boolean | PutLogSettingsPayload) => {
  const body = typeof payload === 'boolean' ? { enabled: payload } : payload
  return request<LogSettings>('/logs/settings', { method: 'PUT', body: JSON.stringify(body) })
}

// ---- Auth ----
export type AuthStatus = {
  auth_required: boolean
  logged_in: boolean
}

export type LoginResponse = {
  token: string
  expires_at: number
}

export const fetchAuthStatus = () => request<AuthStatus>('/auth/status')

export const loginAdmin = async (password: string) => {
  const res = await request<LoginResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
  if (res.token) {
    setAdminToken(res.token)
  }
  return res
}

export const logoutAdmin = async () => {
  try {
    await request<{ status: string }>('/auth/logout', { method: 'POST' })
  } finally {
    clearAdminToken()
    window.dispatchEvent(new CustomEvent('ccrouter:unauthorized'))
  }
}

// ---- MCP Types & APIs ----

export type McpAuthConfig = {
  mode?: 'none' | 'api_key' | 'oauth2'
  type?: 'none' | 'api_key' | 'oauth2'
  isolated?: boolean
  api_key?: string
  header?: string
  header_name?: string
  header_value?: string
  client_id?: string
  client_secret?: string
  auth_url?: string
  authorization_url?: string
  token_url?: string
  scopes?: string[]
  redirect_url?: string
}

export type McpProviderConfig = {
  name: string
  transport: 'stdio' | 'sse' | 'streamablehttp'
  timeout_seconds?: number
  command?: string
  args?: string[]
  env?: Record<string, string>
  working_dir?: string
  url?: string
  headers?: Record<string, string>
  auth_mode?: 'shared' | 'isolated'
  auth?: McpAuthConfig
  proxy?: ProxyConfig
}

export type McpComboMemberConfig = {
  provider: string
  prefix?: string
  tools?: string[]
  resources?: string[]
  prompts?: string[]
}

export type McpComboConfig = {
  name: string
  owned_by?: string
  description?: string
  user_id_header?: string
  members: McpComboMemberConfig[]
}

export type McpToolInfo = {
  name: string
  description?: string
  inputSchema?: {
    type: string
    properties?: Record<string, any>
    required?: string[]
  }
}

export type McpResourceInfo = {
  uri: string
  name: string
  description?: string
  mimeType?: string
}

export type McpPromptArgumentInfo = {
  name: string
  description?: string
  required?: boolean
}

export type McpPromptInfo = {
  name: string
  description?: string
  arguments?: McpPromptArgumentInfo[]
}

export type McpTestResult = {
  success: boolean
  error?: string
  warning?: string
  warnings?: string[]
  server_info?: {
    name: string
    version: string
  }
  tools?: McpToolInfo[]
  resources?: McpResourceInfo[]
  prompts?: McpPromptInfo[]
}

export type McpCapabilitiesResult = McpTestResult

export type McpComboView = {
  name: string
  description: string
  user_id_header: string
  tool_count: number
  tools?: string[]
}

export type McpTokenItem = {
  id: number
  provider: string
  user_id: string
  masked_key: string
  token_type: string
  scopes?: string
  expires_at?: number
  updated_at: number
}

export const getMcpProviders = () => request<{ providers: McpProviderConfig[] }>('/mcp/providers')
export const testMcpProvider = (name: string) => request<McpTestResult>('/mcp/providers/test', { method: 'POST', body: JSON.stringify({ name }) })
export const getMcpProviderCapabilities = (name: string) =>
  request<McpCapabilitiesResult>(`/mcp/providers/${encodeURIComponent(name)}/capabilities`)
export const getMcpCombos = () => request<{ combos: McpComboView[] }>('/mcp/combos')
export const getMcpComboTools = (name: string) => request<{ tools: McpToolInfo[]; error?: string }>(`/mcp/combos/${encodeURIComponent(name)}/tools`)
export const testMcpComboCall = (payload: { combo: string; tool: string; arguments: Record<string, any>; user_id?: string }) =>
  request<{ success: boolean; duration_ms: number; result?: any; error?: any }>('/mcp/combos/call', { method: 'POST', body: JSON.stringify(payload) })
export const getMcpTokens = () => request<{ tokens: McpTokenItem[] }>('/mcp/tokens')
export const deleteMcpToken = (id: number) => request<{ success: boolean }>(`/mcp/tokens/${id}`, { method: 'DELETE' })

// ---- MCP Info ----
export type McpInfoMember = {
  provider: string
  prefix?: string
  tools?: string[]
  resources?: string[]
  prompts?: string[]
}

export type McpInfoCombo = {
  name: string
  owned_by?: string
  description?: string
  user_id_header: string
  tool_count: number
  tools: string[]
  members: McpInfoMember[]
}

export type McpInfoProvider = {
  name: string
  transport: 'stdio' | 'sse' | 'streamablehttp'
  url?: string
  command?: string
  args?: string[]
  working_dir?: string
  auth_mode: 'none' | 'api_key' | 'oauth2'
  isolated: boolean
  timeout_seconds?: number
  headers?: string[]
  is_ready: boolean
}

export type McpAdminInfo = {
  version: string
  runtime: string
  external_url?: string
  combos: McpInfoCombo[]
  providers: McpInfoProvider[]
  total_tools: number
  tokens_count: number
  user_count: number
  provider_tokens: Record<string, number>
}

export const getMcpAdminInfo = () => request<McpAdminInfo>('/mcp/info')

// ---- MCP Requests (Audit Log) ----
export type McpRequestRow = {
  id: number
  ts: number
  combo: string
  transport: string
  method: string
  tool_name: string | null
  provider: string | null
  user_id: string | null
  arguments: string | null
  result: string | null
  duration_ms: number | null
  status_code: number | null
  success: number
  error: string | null
}

export type McpRequestsResp = {
  total: number
  items: McpRequestRow[]
}

export const getMcpRequests = (params: {
  limit?: number
  offset?: number
  combo?: string
  provider?: string
  tool?: string
  user_id?: string
  method?: string
  transport?: string
  q?: string
  success?: boolean
  since?: number
  until?: number
}) => {
  const qs = new URLSearchParams()
  Object.entries(params).forEach(([k, v]) => {
    if (v != null) qs.set(k, String(v))
  })
  return request<McpRequestsResp>(`/mcp/requests?${qs}`)
}

// ---- MCP Overview & Stats ----
export type McpOverviewStats = {
  total_calls: number
  success_calls: number
  error_calls: number
  avg_duration_ms: number
  unique_tools: number
  active_providers: number
  active_combos: number
  active_users: number
}

export type McpSummaryRow = {
  group_key: string
  total: number
  success_count: number
  error_count: number
  avg_duration_ms: number
  min_duration_ms?: number
  max_duration_ms?: number
}

export type McpTrendRow = {
  bucket_ts: number
  total: number
  success_count: number
}

export const getMcpStatsSummary = (params?: { group_by?: string; since?: number; until?: number }) => {
  const qs = new URLSearchParams()
  if (params) {
    Object.entries(params).forEach(([k, v]) => {
      if (v != null) qs.set(k, String(v))
    })
  }
  return request<{ data: McpSummaryRow[]; overview: McpOverviewStats; group_by: string }>(`/mcp/stats/summary?${qs}`)
}

export const getMcpStatsTrend = (params?: { bucket?: string; since?: number; until?: number }) => {
  const qs = new URLSearchParams()
  if (params) {
    Object.entries(params).forEach(([k, v]) => {
      if (v != null) qs.set(k, String(v))
    })
  }
  return request<{ data: McpTrendRow[]; bucket: string }>(`/mcp/stats/trend?${qs}`)
}

// Modular Config Endpoints (common / model / mcp)
export type McpConfigSection = {
  mcp_providers: McpProviderConfig[]
  mcp_combos: McpComboConfig[]
}

export const getMcpConfig = () => request<McpConfigSection>('/config/mcp')
export const putMcpConfig = (payload: McpConfigSection) =>
  request<McpConfigSection>('/config/mcp', { method: 'PUT', body: JSON.stringify(payload) })

export type ModelConfigSection = {
  providers: ProviderConfig[]
  combos: ComboConfig[]
}

export const getModelConfig = () => request<ModelConfigSection>('/config/model')
export const putModelConfig = (payload: ModelConfigSection) =>
  request<ModelConfigSection>('/config/model', { method: 'PUT', body: JSON.stringify(payload) })

export type CommonConfigSection = {
  general: GeneralConfig
  logging: LoggingConfig
  verbose_logging: boolean
  payload_scripts: PayloadScript[]
}

export const getCommonConfig = () => request<CommonConfigSection>('/config/common')
export const putCommonConfig = (payload: Partial<CommonConfigSection>) =>
  request<CommonConfigSection>('/config/common', { method: 'PUT', body: JSON.stringify(payload) })

export const patchConfig = (patch: Record<string, any>) =>
  request<AppConfig>('/config', { method: 'PATCH', body: JSON.stringify(patch) })

export const EMPTY_MCP_PROVIDER = (): McpProviderConfig => ({
  name: '',
  transport: 'stdio',
  timeout_seconds: 30,
  command: '',
  args: [],
  env: {},
  working_dir: '',
  url: '',
  headers: {},
  auth_mode: 'shared',
  auth: {
    mode: 'none',
    isolated: false,
    header: 'Authorization',
    api_key: '',
    client_id: '',
    client_secret: '',
    auth_url: '',
    token_url: '',
    scopes: [],
  },
})

export const EMPTY_MCP_COMBO = (): McpComboConfig => ({
  name: '',
  description: '',
  user_id_header: 'X-User-Id',
  members: [
    {
      provider: '',
      prefix: '',
      tools: ['*'],
    },
  ],
})

