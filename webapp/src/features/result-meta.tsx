import type { Coverage, Item, Metric, QueryResponse, Route } from '../lib/api'
import type { Stage } from '../components/pipeline'
import { Dot } from '../components/badge'
import { cn } from '../lib/utils'

// 执行过程里的阶段名（trace.stage）→ 中文与路线色。未登记的阶段原样显示。
const stageNames: Record<string, { label: string; route?: Stage['route']; unit?: string }> = {
  published_view: { label: '已发布视图', unit: '块' },
  query_projection: { label: '元数据投影' },
  indexed_scope: { label: '索引范围', unit: '文件' },
  index_match: { label: '索引命中', route: 'kw' },
  result_window: { label: '结果窗口' },
  keyword: { label: '关键词', route: 'kw' },
  keyword_candidate_window: { label: '关键词候选上限' },
  semantic: { label: '语义', route: 'sem' },
  graph_supplement: { label: '图补充', route: 'gr' },
  graph_frontier: { label: '图前沿', route: 'gr' },
  external: { label: '外部检索' },
  fusion: { label: 'RRF 融合' },
  rerank: { label: '重排' },
  per_file: { label: '每文件上限' },
  result: { label: '返回' }
}

export function pipelineStages(response: QueryResponse, semanticConfigured: boolean): Stage[] {
  const steps = response.trace ?? []
  const stages: Stage[] = steps.map(step => {
    const info = stageNames[step.stage] ?? { label: step.stage }
    return { label: info.label, value: step.count, unit: info.unit, route: info.route, title: step.note ? translateNote(step.note) ?? undefined : undefined }
  })
  // 组合检索在未配置语义模型时不会产生语义阶段；补一格“跳过”，避免误以为语义路线执行后没有结果。
  if (response.action === '+ask' && !semanticConfigured && !steps.some(s => s.stage === 'semantic')) {
    const at = steps.findIndex(s => s.stage === 'keyword')
    stages.splice(at >= 0 ? at + 1 : stages.length, 0, { label: '语义 · 跳过', value: '—', route: 'sem', skipped: true, title: '未配置向量模型，未执行' })
  }
  return stages
}

export function totalDuration(response: QueryResponse) {
  const ms = (response.trace ?? []).reduce((sum, step) => sum + (step.duration_ms || 0), 0)
  return `${ms} ms`
}

// 页面展示影响结果或下一步操作的说明，其他诊断信息保留在完整响应中。
const noteText: [RegExp, (m: RegExpMatchArray) => string | null][] = [
  [/^Semantic is not configured; this run uses lexical retrieval\.?$/, () => '未配置向量模型，本次使用关键词检索'],
  [/^semantic branch unavailable: (.+)$/, m => `语义检索失败：${m[1]}`],
  [/^per_file limits selected results, not the complete keyword count$/, () => '已按每个文件的条数上限筛选结果'],
  [/^The saved result window is bounded; total accuracy and scope are stated separately\. Narrow the query for additional members\.$/, () => '结果超过保存上限，请缩小检索范围。'],
  [/^hybrid retrieval uses at most (\d+) lexical candidates; not an exhaustive result$/, m => `关键词候选上限：${m[1]} 条`],
  [/^removed (\d+) candidates by per-file limit$/, m => Number(m[1]) > 0 ? `按每文件上限移除 ${m[1]} 条候选` : null],
  [/^Scope was explicitly expanded to mentions in the selected knowledge bases\.$/, () => '已扩展到相关实体的提及片段']
]

export function translateNote(note: string) {
  for (const [pattern, render] of noteText) {
    const match = note.match(pattern)
    if (match) return render(match)
  }
  return null
}

export const unitLabels: Record<string, string> = {
  chunks: '块',
  files: '个文件',
  matches: '处匹配',
  relations: '条关系',
  paths: '条路径',
  entities: '个实体',
  results: '条',
  evidence: '条依据'
}

export function totalText(total: Metric) {
  if (total.value == null) return '不提供'
  const prefix = total.accuracy === 'lower_bound' ? '至少 ' : total.accuracy === 'estimate' ? '约 ' : ''
  return `${prefix}${total.value} ${unitLabels[total.unit] ?? total.unit}`
}

const consistencyLabels: Record<string, string> = {
  file_view: '按文件版本',
  mixed_live: '混合实时',
  direct_live: '直连实时',
  filesystem_live: '本地实时'
}

const scopeLabels: Record<string, string> = {
  corpus: '全库',
  enumerated: '指定范围',
  candidate: '候选集合',
  expanded: '扩展范围'
}

/** 四种完整性（文档 V0.8 §04.10）：总数、执行、近似、展示，外加一致性与范围。 */
export function Completeness({ response, className }: { response: QueryResponse; className?: string }) {
  const c: Coverage = response.coverage
  const rows: { label: string; value: string; tone?: 'ok' | 'warn' | 'off'; title?: string }[] = [
    {
      label: '总数',
      value: totalText(response.total),
      tone: response.total.value == null ? 'off' : 'ok',
      title: response.total.value == null ? '这一动作不统计全集总数' : `统计范围：${response.total.scope}`
    },
    { label: '执行', value: c.execution_partial ? '部分' : '完整', tone: c.execution_partial ? 'warn' : 'ok' },
    { label: '近似', value: c.approximate ? '是' : '否', tone: c.approximate ? 'warn' : 'ok', title: c.approximate ? '向量近邻检索不穷举' : undefined },
    { label: '展示', value: c.display_truncated ? '已截断' : '未截断', tone: c.display_truncated ? 'warn' : 'ok' },
    { label: '一致性', value: consistencyLabels[c.consistency] ?? c.consistency },
    { label: '范围', value: scopeLabels[c.scope_basis] ?? c.scope_basis }
  ]
  return (
    <div aria-label="结果完整性" className={cn('flex flex-wrap gap-x-5 gap-y-1.5 text-xs text-faint', className)}>
      {rows.map(row => (
        <span key={row.label} className="inline-flex items-center gap-1.5" title={row.title}>
          {row.tone && <Dot tone={row.tone} />}
          {row.label}
          <b className="font-medium text-fg">{row.value}</b>
        </span>
      ))}
    </div>
  )
}

const actionRoute: Record<string, Route> = {
  'search.keyword': 'keyword',
  'search.semantic': 'semantic',
  'search.external': 'external'
}

export function itemRoutes(item: Item, action: string): Route[] {
  if (item.routes?.length) return item.routes
  const route = actionRoute[action]
  return route ? [route] : []
}

export const rankingLabels: Record<string, string> = {
  rrf: 'rrf',
  bm25: 'bm25',
  reranker: '重排',
  scan_heuristic: '扫描',
  none: '无排序'
}

/** 结果条目的标题、小节和摘要。文本块开头的 Markdown 标题作为小节名。 */
export function itemView(item: Item) {
  const title = (item.title || item.name || item.path || '结果').replace(/^#+\s*/, '')
  const raw = (item.text ?? '').replace(/\r\n/g, '\n').trim()
  const [first, ...others] = raw.split('\n')
  const heading = first?.match(/^#{1,6}\s+(.+)$/)
  const section = heading && heading[1].trim() !== title ? heading[1].trim() : ''
  const body = (heading ? others.join('\n') : raw)
    .replace(/^#{1,6}\s+/gm, '')
    .replace(/\n{2,}/g, '\n')
    .trim()
  return { title, section, snippet: body }
}
