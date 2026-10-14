import type { ActionSpec } from './api'
import { shellArg } from './utils'

// 把网页上发出的一次查询，写成等价的 CLI、HTTP 和 MCP 调用。规则与 internal/cli 的参数解析一致。

const positional: Record<string, string> = {
  'search.keyword': 'q',
  'search.semantic': 'q',
  'search.external': 'q',
  '+ask': 'q',
  'files.grep': 'pattern',
  'graph.entity': 'name',
  'graph.neighbors': 'name',
  'graph.chunks': 'name',
  '+context': 'name',
  'files.read': 'ref'
}

const flags: Record<string, string> = {
  limit: 'limit',
  max_chars: 'max-chars',
  length: 'length',
  offset: 'offset',
  trace: 'trace',
  graph: 'graph',
  rerank: 'rerank',
  save_scope: 'save-scope',
  within: 'within',
  path: 'path',
  ref: 'ref',
  name: 'name',
  id: 'id',
  direction: 'direction',
  from_id: 'from',
  to_id: 'to',
  cursor: 'cursor',
  result_id: 'result'
}

export type Invocation = { cli: string; http: string; mcp: string }

export function invocation(spec: ActionSpec, body: Record<string, unknown>, profile: string): Invocation {
  const parts = ['xrag', spec.cli]
  const rest: Record<string, unknown> = {}
  const posKey = positional[spec.action]
  if (posKey && typeof body[posKey] === 'string') parts.push(shellArg(body[posKey] as string))
  const kbs = body.kbs as string[] | undefined
  if (kbs?.length) parts.push('--kb', shellArg(kbs.join(',')))
  for (const [key, value] of Object.entries(body)) {
    if (key === 'kbs' || key === posKey || value === undefined) continue
    const flag = spec.action === 'graph.path' && key === 'max_hops' ? 'hops' : key === 'hops' && spec.action !== 'graph.path' ? 'hops' : flags[key]
    if (flag && typeof value === 'boolean') {
      if (value) parts.push(`--${flag}`)
    } else if (flag && (typeof value === 'string' || typeof value === 'number')) {
      parts.push(`--${flag}`, shellArg(String(value)))
    } else {
      rest[key] = value
    }
  }
  if (Object.keys(rest).length) parts.push('--data', shellArg(JSON.stringify(rest)))
  parts.push('--profile', shellArg(profile))

  return {
    cli: parts.join(' '),
    http: `POST ${spec.path}\nAuthorization: Bearer $XRAG_KEY\nContent-Type: application/json\n\n${JSON.stringify(body, null, 2)}`,
    mcp: JSON.stringify({ name: spec.tool, arguments: body }, null, 2)
  }
}
