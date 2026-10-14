// XRAG HTTP 接口。字段名与服务端 JSON 保持一致，前端不做别名转换。

export type Role = 'reader' | 'contributor' | 'maintainer' | 'admin'

export type Identity = {
  id: string
  name: string
  role: Role
  kbs: string[]
}

export type KbCaps = {
  embedding?: boolean
  external?: boolean
  graph_backend?: string
  graph_mode?: string
  parser?: string
  qdrant?: boolean
  rerank?: boolean
  wiki?: boolean
  settings_revision?: number
  configuration_error?: string
}

export type SystemInfo = {
  identity: Identity
  version: string
  workers: boolean
  embedding: boolean
  rerank: boolean
  wiki: boolean
  qdrant: boolean
  graph_backend: string
  keyword?: string
  storage?: string
  edge_login?: boolean
  by_kb: Record<string, KbCaps>
  plugins?: Record<string, { kind: string; version: string; protocol?: string }>
  mcp?: {
    enabled: boolean
    versions: string[]
    http_post: string[]
    resources: boolean
    prompts: boolean
    oauth_resource_server: boolean
    server_initiated_stream: boolean
  }
}

export type KnowledgeBase = { id: string; name: string; files?: number }

export type ActionSpec = {
  action: string
  path: string
  cli: string
  tool: string
  group: string
  description: string
}

export type Metric = {
  value: number | null
  accuracy: 'exact' | 'unknown' | 'lower_bound' | 'estimate'
  unit: string
  calculation_complete: boolean
  scope: string
}

export type Coverage = {
  execution_partial: boolean
  display_truncated: boolean
  approximate: boolean
  consistency: string
  scope_basis: string
}

export type TraceStep = { stage: string; count: number; duration_ms: number; note?: string; ids?: string[] }

export type Edge = {
  support_id: string
  kb: string
  path: string
  edition_id: string
  chunk_id: string
  from: string
  to: string
  relation: string
  evidence?: string
  origin?: string
}

export type Route = 'keyword' | 'semantic' | 'graph' | 'external'

export type Item = {
  kind: string
  ref?: string
  kb?: string
  path?: string
  edition_id?: string
  chunk_id?: string
  title?: string
  name?: string
  text?: string
  score?: number
  ranking?: string
  routes?: Route[]
  line_start?: number
  line_end?: number
  fields?: Record<string, unknown>
  from?: string
  to?: string
  relation?: string
  support_count?: number
  edges?: Edge[]
  nodes?: string[]
  support_refs?: string[]
  text_available?: boolean
}

export type QueryResponse<D = unknown> = {
  schema_version: string
  query_id: string
  action: string
  mode: string
  kbs: string[]
  items: Item[]
  returned: number
  total: Metric
  coverage: Coverage
  handle?: string
  result_id?: string
  next_cursor?: string
  cache_complete: boolean
  strategy: Record<string, unknown>
  notes: string[]
  trace?: TraceStep[]
  data?: D
  summary_ref?: string
}

export type ReadWindow = {
  line_start: number
  line_end: number
  cite_start: number
  cite_end: number
  total_lines: number
  text: string
}

export type ReadData = {
  ref: string
  kb: string
  path: string
  edition_id: string
  text: string
  offset: number
  length: number
  total_chars: number
  next_offset: number | null
  freshness: string
  availability: string
  kind: string
  window?: ReadWindow
}

export type JobState = 'queued' | 'running' | 'succeeded' | 'failed' | 'partial' | 'cancelled'

export type Job = {
  id: string
  kb: string
  kind: string
  state: JobState | string
  payload?: Record<string, any>
  result?: Record<string, any>
  error?: string
  owner?: string
  created: number
  first_started_at?: number
  attempt: number
  cancel_requested?: boolean
}

export type SourceDetail = {
  source: {
    kb: string
    id: string
    root: string
    mount: string
    kind: string
    git_ref?: string
    options?: { revision?: number; disabled?: boolean; include?: string[]; exclude?: string[] }
  }
  policy: {
    every_seconds: number
    watch_seconds: number
    debounce_seconds: number
    hook_secret_ref?: string
    hook_provider?: string
    hook_repository?: string
    hook_ref?: string
    schedule?: string
    timezone?: string
    native_watch?: boolean
  }
  status: {
    last_checked?: number
    last_success?: number
    source_revision?: string
    indexed_fingerprint?: string
    observed_fingerprint?: string
    error?: string
  }
}

export type KnowledgeItem = {
  id: string
  kb: string
  title: string
  kind: string
  state: string
  draft_revision: string
  published_revision?: string
  body_md?: string
  refs?: string[]
  applies_to?: string
  not_applies_to?: string
  updated?: number
}

export type SyncTarget = { id: string; name: string; kind: string; root: string; prefix: string; branch?: string; auto_sync?: boolean }

export type Feedback = {
  id: string
  kb: string
  ref?: string
  path?: string
  label: string
  note?: string
  status: 'open' | 'resolved' | 'dismissed'
  resolution?: string
  revision: number
  created: number
}

export type EvalCase = { id: string; question: string; expected_empty?: boolean; expected?: { path: string; anchor?: string }[] }
export type EvalSet = { id: string; kb: string; name: string; revision: string; purpose?: string; family?: string; cases: EvalCase[] }
export type EvalDraft = { id: string; status: string; revision: number; feedback_id: string; case: EvalCase }

export type EvalSummary = {
  strategy: string
  hit_at_k: number
  recall_at_k: number
  mrr: number
  negative_accuracy: number | null
  errors: number
  cases: number
  p95_ms: number
}

export type EvalReport = {
  set_id: string
  k: number
  manifest_hash: string
  settings_hash: string
  summaries: EvalSummary[]
  pairs: { a: string; b: string; only_a: string[]; only_b: string[] }[]
  rows: {
    case_id: string
    strategy: string
    question: string
    error?: { code: string; message: string }
    expected_empty?: boolean
    score: { hit: boolean; empty_correct?: boolean; missed_paths: string[] }
    actual_strategy?: unknown
    items?: unknown
    trace?: unknown
  }[]
  message?: string
}

export type QueryRecord = {
  id: string
  kbs: string[]
  created: number
  request: Record<string, any>
  response: QueryResponse
}

export type ApiKey = {
  id: string
  name: string
  role: Role
  kbs: string[]
  created: number
  expires_at: number
  disabled: boolean
  state: string
}

export type Backup = { id: string; size: number; created: number; available: boolean; sha256: string }

export type Endpoint = Record<string, string | number | undefined>
export type ProviderConfig = Partial<Record<'embedding' | 'chat' | 'rerank' | 'qdrant' | 'neo4j', Endpoint>> & Record<string, unknown>

export type ProviderState = {
  editable: boolean
  config: ProviderConfig
  active: ProviderConfig
  restart_required: boolean
  source: string
  revision: number
  updated: number
  note?: string
}

export type KbSettings = {
  revision: number
  parser?: string
  parser_plugin?: string
  chunk_max_chars?: number
  dictionary?: string[]
  fields?: Record<string, string>
  graph?: {
    mode?: string
    plugin?: string
    entity_types?: string[]
    relation_types?: string[]
    include?: string[]
    exclude?: string[]
    optional?: boolean
  }
  budget?: { max_calls_per_run?: number; max_tokens_per_run?: number; max_tokens_per_day?: number }
  providers?: unknown
  [key: string]: unknown
}

export type MaintenancePolicy = {
  enabled: boolean
  backup_every_seconds: number
  backup_keep: number
  records_every_seconds: number
  records_keep_days: number
  batch_size: number
  content_every_seconds: number
  content_keep_days: number
  content_external: boolean
}

export type Overview = {
  counts: { files: number; knowledge: number; source_errors: number; sources: number }
  database_bytes?: Record<string, number>
  jobs: Partial<Record<string, number>>
  version: string
  workers: boolean
  kbs: string[]
}

export class ApiError extends Error {
  status: number
  code: string

  constructor(message: string, status: number, code: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal
  })
  let payload: any
  try {
    payload = await response.json()
  } catch {
    throw new ApiError(`服务返回了非 JSON 内容（HTTP ${response.status}）`, response.status, 'BAD_RESPONSE')
  }
  if (!response.ok) {
    const error = payload?.error
    const code = typeof error === 'object' && error?.code ? error.code : String(response.status)
    const message = typeof error === 'object' ? error?.message : error
    throw new ApiError(message || '请求失败', response.status, code)
  }
  return payload as T
}

const get = <T>(path: string, signal?: AbortSignal) => request<T>('GET', path, undefined, signal)
const post = <T>(path: string, body: unknown = {}) => request<T>('POST', path, body)
const put = <T>(path: string, body: unknown) => request<T>('PUT', path, body)
const patch = <T>(path: string, body: unknown) => request<T>('PATCH', path, body)
const del = <T>(path: string, body: unknown = {}) => request<T>('DELETE', path, body)

const q = (params: Record<string, string | number | undefined>) => {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) if (value !== undefined && value !== '') search.set(key, String(value))
  const text = search.toString()
  return text ? `?${text}` : ''
}

const kbPath = (kb: string) => `/v1/kbs/${encodeURIComponent(kb)}`

export async function download(path: string, filename: string) {
  const response = await fetch(path, { credentials: 'same-origin' })
  if (!response.ok) {
    let message = `下载失败：HTTP ${response.status}`
    try {
      message = (await response.json())?.error?.message ?? message
    } catch {
      // 非 JSON 错误体沿用状态码说明
    }
    throw new ApiError(message, response.status, 'DOWNLOAD_FAILED')
  }
  const url = URL.createObjectURL(await response.blob())
  const link = Object.assign(document.createElement('a'), { href: url, download: filename })
  document.body.append(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 30000)
}

export function downloadJSON(value: unknown, filename: string) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(value, null, 2)], { type: 'application/json' }))
  const link = Object.assign(document.createElement('a'), { href: url, download: filename })
  link.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export const api = {
  // 会话
  loginMode: () => get<{ mode: 'key' | 'password' }>('/v1/login'),
  login: (body: { key: string } | { username: string; password: string }) => post<Identity>('/v1/session', body),
  logout: () => del<{ logged_out: boolean }>('/v1/session'),
  system: () => get<SystemInfo>('/v1/system'),
  schema: () => get<{ actions: ActionSpec[] }>('/v1/schema'),
  overview: () => get<Overview>('/v1/overview'),

  // 知识库
  kbs: () => get<{ items: KnowledgeBase[] }>('/v1/kbs'),
  createKb: (id: string, name: string) => post('/v1/kbs', { id, name }),
  renameKb: (kb: string, name: string) => patch(kbPath(kb), { name }),
  deleteKb: (kb: string) => del(kbPath(kb), { confirm: true }),
  kbSettings: (kb: string) => get<KbSettings>(`${kbPath(kb)}/settings`),
  saveKbSettings: (kb: string, expected_revision: number, settings: KbSettings) =>
    put<KbSettings>(`${kbPath(kb)}/settings`, { expected_revision, settings }),
  usage: (kb: string) => get<{ totals: { operation: string; state: string; calls: number; tokens: number }[]; unit: string; reservation_basis: string }>(`${kbPath(kb)}/usage`),
  exportPath: (kb: string) => `${kbPath(kb)}/export`,

  // 查询动作
  query: <D = unknown>(path: string, body: Record<string, unknown>) => post<QueryResponse<D>>(path, body),
  page: (kb: string, result_id: string, cursor: string, limit: number) =>
    post<QueryResponse>('/v1/results/page', { kbs: [kb], result_id, cursor, limit, max_chars: 8000 }),
  readRef: (kb: string, ref: string, options: { offset?: number; length?: number; context_lines?: number } = {}) =>
    post<QueryResponse<ReadData>>('/v1/files/read', { kbs: [kb], ref, offset: 0, length: 6000, ...options }),
  tree: (kb: string, body: Record<string, unknown> = {}) => post<QueryResponse>('/v1/files/tree', { kbs: [kb], limit: 100, save_scope: 'auto', ...body }),
  discover: (kb: string) => post<QueryResponse<Record<string, any>>>('/v1/discover', { kbs: [kb] }),

  // 资料、来源与构建
  sources: (kb: string) => get<{ details: SourceDetail[] }>(`${kbPath(kb)}/sources`),
  addSource: (kb: string, body: Record<string, unknown>) => post(`${kbPath(kb)}/sources`, body),
  editSource: (kb: string, id: string, body: Record<string, unknown>) => patch(`${kbPath(kb)}/sources/${encodeURIComponent(id)}`, body),
  removeSource: (kb: string, id: string, expected_revision: number) =>
    del(`${kbPath(kb)}/sources/${encodeURIComponent(id)}`, { expected_revision, confirm: true }),
  sourcePolicy: (kb: string, id: string, policy: SourceDetail['policy']) => put(`${kbPath(kb)}/sources/${encodeURIComponent(id)}/policy`, policy),
  build: (kb: string, mode: string) => post<{ id: string }>(`${kbPath(kb)}/builds`, { mode }),
  estimate: (kb: string) => post<Record<string, unknown>>(`${kbPath(kb)}/build-estimates`, {}),
  upload: (kb: string, path: string, body: string) => post<{ id: string }>(`${kbPath(kb)}/upload`, { path, body }),

  // 任务
  jobs: (filter: { kb?: string; kind?: string } = {}, signal?: AbortSignal) => get<{ items: Job[] }>(`/v1/jobs${q(filter)}`, signal),
  job: (id: string) => get<Job>(`/v1/jobs/${encodeURIComponent(id)}`),
  jobDetails: (id: string) => get<Record<string, unknown>>(`/v1/jobs/${encodeURIComponent(id)}/details`),
  cancelJob: (id: string) => post(`/v1/jobs/${encodeURIComponent(id)}/cancel`, {}),
  resumeTask: (id: string) => post('/v1/tasks/resume', { id }),
  research: (body: Record<string, unknown>) => post<{ id: string }>('/v1/research', body),
  recipe: (body: Record<string, unknown>) => post<{ id: string }>('/v1/recipes/run', body),

  // 知识与 Wiki
  knowledge: (kb: string) => get<{ items: KnowledgeItem[] }>(`${kbPath(kb)}/knowledge`),
  knowledgeItem: (id: string) => get<KnowledgeItem>(`/v1/knowledge/${encodeURIComponent(id)}`),
  saveKnowledge: (kb: string, body: Record<string, unknown>) => post<KnowledgeItem>(`${kbPath(kb)}/knowledge`, body),
  publishKnowledge: (id: string, revision: string) => post<{ id: string }>(`/v1/knowledge/${encodeURIComponent(id)}/publish`, { revision }),
  archiveKnowledge: (id: string, revision: string) => post(`/v1/knowledge/${encodeURIComponent(id)}/archive`, { revision, confirm: true }),
  knowledgeRevisions: (id: string) => get<Record<string, unknown>>(`/v1/knowledge/${encodeURIComponent(id)}/revisions`),
  knowledgeSyncs: (id: string) => get<Record<string, unknown>>(`/v1/knowledge/${encodeURIComponent(id)}/sync`),
  syncKnowledge: (id: string, target_id: string, revision: string) =>
    post<{ id: string }>(`/v1/knowledge/${encodeURIComponent(id)}/sync`, { target_id, revision, confirm: true }),
  regenerate: (id: string, body: { topic: string; path: string; expected_revision: string }) =>
    post<{ id: string }>(`/v1/knowledge/${encodeURIComponent(id)}/regenerate`, body),
  wiki: (kb: string, topic: string) => post<{ id: string }>(`${kbPath(kb)}/wiki`, { topic }),
  syncTargets: (kb: string) => get<{ items: SyncTarget[]; automatic_sync?: boolean }>(`${kbPath(kb)}/sync-targets`),
  addSyncTarget: (kb: string, target: Record<string, unknown>) => post(`${kbPath(kb)}/sync-targets`, { target, confirm: true }),

  // 反馈
  feedback: (params: { kb: string; status?: string; offset?: number; limit?: number }) =>
    get<{ items: Feedback[]; total: number; next_offset: number | null }>(`/v1/feedback${q({ limit: 20, ...params })}`),
  sendFeedback: (body: { kb: string; ref: string; label: string; note: string }) => post('/v1/feedback', body),
  resolveFeedback: (id: string, body: { status: string; resolution: string; expected_revision: number }) =>
    patch(`/v1/feedback/${encodeURIComponent(id)}`, body),
  feedbackToCase: (id: string, question: string) => post<{ id: string }>(`/v1/feedback/${encodeURIComponent(id)}/test-case`, { question }),

  // 评测与查询记录
  evalSets: () => get<{ items: EvalSet[] }>('/v1/eval/sets'),
  evalSet: (id: string) => get<EvalSet>(`/v1/eval/sets/${encodeURIComponent(id)}`),
  importEvalSet: (body: Record<string, unknown>) => post<EvalSet>('/v1/eval/sets', body),
  runEval: (body: { set_id: string; k: number; strategies: string[] }) => post<{ id: string }>('/v1/eval/runs', body),
  evalDrafts: (kb: string) => get<{ items: EvalDraft[] }>(`/v1/eval/drafts${q({ kb })}`),
  approveDraft: (id: string, body: Record<string, unknown>) => post(`/v1/eval/drafts/${encodeURIComponent(id)}/approve`, body),
  setFromDrafts: (body: Record<string, unknown>) => post<EvalSet>('/v1/eval/sets/from-drafts', body),
  queries: () => get<{ items: QueryRecord[] }>('/v1/queries'),
  queryRecord: (id: string) => get<{ items: QueryRecord[] }>(`/v1/queries/${encodeURIComponent(id)}`),
  diagnose: (id: string, body: { kb: string; path: string; anchor: string }) => post<Record<string, unknown>>(`/v1/queries/${encodeURIComponent(id)}/diagnose`, body),

  // 接入
  keys: () => get<{ items: ApiKey[]; current_key_id?: string }>('/v1/keys'),
  createKey: (body: { name: string; role: Role; kbs: string[]; expires_at: number }) => post<{ key: string }>('/v1/keys', body),
  renameKey: (id: string, name: string) => patch(`/v1/keys/${encodeURIComponent(id)}`, { name }),
  disableKey: (id: string) => del(`/v1/keys/${encodeURIComponent(id)}`, { confirm: true }),

  // 设置与数据
  providers: () => get<ProviderState>('/v1/settings/providers'),
  saveProviders: (expected_revision: number, config: ProviderConfig) =>
    put<ProviderState>('/v1/settings/providers', { expected_revision, config }),
  backups: () => get<{ items: Backup[]; max_snapshots: number; scope: string }>('/v1/backups'),
  createBackup: () => post<Backup>('/v1/backups', { confirm: true }),
  removeBackup: (id: string) => del(`/v1/backups/${encodeURIComponent(id)}`, { confirm: true }),
  backupPath: (id: string) => `/v1/backups/${encodeURIComponent(id)}/download`,
  cachePreview: () => get<Record<string, unknown>>('/v1/maintenance/cache'),
  cacheClean: () => post<Record<string, unknown>>('/v1/maintenance/cache', { confirm: true }),
  maintenancePolicy: () =>
    get<{ revision: number; policy: MaintenancePolicy; next_backup_at: number; next_records_at: number; next_content_at: number }>('/v1/maintenance/policy'),
  saveMaintenancePolicy: (expected_revision: number, policy: MaintenancePolicy) => put('/v1/maintenance/policy', { expected_revision, policy }),
  maintenanceRuns: () => get<{ items: { id: string; state: string; created: number; automatic?: boolean; backup?: boolean; records?: boolean; content?: boolean; error?: string; result?: unknown }[] }>('/v1/maintenance/runs'),
  runMaintenance: (body: { backup?: boolean; records?: boolean; content?: boolean }) => post('/v1/maintenance/runs', { confirm: true, ...body }),
  maintenanceRecords: () => get<Record<string, unknown>>('/v1/maintenance/records'),
  maintenanceContent: () => get<Record<string, unknown>>('/v1/maintenance/content')
}
