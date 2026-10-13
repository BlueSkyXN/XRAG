export type Identity = {
  name: string
  role: string
}

export type SystemInfo = {
  identity: Identity
  embedding?: boolean
  rerank?: boolean
  wiki?: boolean
  workers?: boolean
  graph_backend?: string
  version?: string
}

export type KnowledgeBase = {
  id: string
  name: string
}

export type ResultItem = {
  kind?: string
  title?: string
  name?: string
  path?: string
  text?: string
  score?: number
  ranking?: string
  ref?: string
  edition_id?: string
  line_start?: number
  line_end?: number
}

export type SearchResult = {
  query_id: string
  returned: number
  mode: string
  result_id?: string
  next_cursor?: string | null
  total?: { value: number | null; unit?: string }
  items: ResultItem[]
}

export type LoginMode = 'key' | 'password'
export type SearchMode = 'ask' | 'keyword' | 'semantic'

export class ApiError extends Error {}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: 'same-origin',
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers
    }
  })
  let payload: unknown
  try {
    payload = await response.json()
  } catch {
    throw new ApiError('服务没有返回 JSON')
  }
  if (!response.ok) {
    const detail = payload as { error?: { code?: string; message?: string } | string }
    const error = typeof detail.error === 'string' ? detail.error : detail.error
    const code = error && typeof error === 'object' ? error.code : response.status
    const message =
      error && typeof error === 'object' ? error.message : String(error ?? '请求失败')
    throw new ApiError(`${code ?? response.status} · ${message}`)
  }
  return payload as T
}

export const api = {
  loginMode: () => request<{ mode: LoginMode }>('/v1/login'),
  loginWithKey: (key: string) =>
    request<Identity>('/v1/session', {
      method: 'POST',
      body: JSON.stringify({ key })
    }),
  loginWithPassword: (username: string, password: string) =>
    request<Identity>('/v1/session', {
      method: 'POST',
      body: JSON.stringify({ username, password })
    }),
  logout: () => request<{ logged_out: boolean }>('/v1/session', { method: 'DELETE' }),
  system: () => request<SystemInfo>('/v1/system'),
  kbs: () => request<{ items: KnowledgeBase[] }>('/v1/kbs'),
  search: (mode: SearchMode, body: Record<string, unknown>) => {
    const paths: Record<SearchMode, string> = {
      ask: '/v1/ask',
      keyword: '/v1/search/keyword',
      semantic: '/v1/search/semantic'
    }
    return request<SearchResult>(paths[mode], { method: 'POST', body: JSON.stringify(body) })
  },
  read: (kb: string, ref: string) =>
    request<{ data: { path?: string; text?: string; edition_id?: string; freshness?: string } }>(
      '/v1/files/read',
      {
        method: 'POST',
        body: JSON.stringify({ kbs: [kb], ref, offset: 0, length: 6000 })
      }
    )
}
