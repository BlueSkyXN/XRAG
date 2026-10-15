import { SyntheticEvent, useEffect, useRef, useState } from 'react'
import { EyeOff, Radio, RotateCcw, Square } from 'lucide-react'
import { api, Job } from '../lib/api'
import { useData, useFeedback, usePolling, useRun } from '../lib/feedback'
import { useSession } from '../lib/session'
import { cn, shortTime, tail, when } from '../lib/utils'
import { Badge, StateBadge } from '../components/badge'
import { Button } from '../components/button'
import { Segmented } from '../components/chip'
import { JsonBlock } from '../components/code'
import { Field, Input, Textarea } from '../components/input'
import { Page, PageHeader } from '../components/page'
import { Empty, ErrorLine, KeyValues, Note, Panel, PanelBody, PanelHeader } from '../components/panel'
import { activeStates } from '../features/jobs'
import { ReaderSheet, ReadTarget } from '../features/reader'

const defaultRecipe = {
  version: 'xrag.recipe/v1',
  name: 'keyword-then-read',
  steps: [
    { action: 'search.keyword', arguments: { q: '$input', limit: 3 } },
    { action: 'files.read', arguments: { ref: '$last.ref', length: 3000 } }
  ]
}

type Evidence = { id: string; kind: string; ref?: string; metric?: { value: unknown; unit: string }; content?: unknown }

function TaskDetail({ id, onChanged }: { id: string; onChanged: () => void }) {
  const run = useRun()
  const { confirm } = useFeedback()
  const { kb } = useSession()
  const job = useData(() => api.job(id), [id])
  const [live, setLive] = useState<{ job?: Job; status: string } | null>(null)
  const [reading, setReading] = useState<ReadTarget | null>(null)
  const streamRef = useRef<EventSource | null>(null)

  const stop = (status = '已停止查看；任务继续执行') => {
    streamRef.current?.close()
    streamRef.current = null
    setLive(l => (l ? { ...l, status } : null))
  }

  useEffect(() => () => streamRef.current?.close(), [])
  useEffect(() => {
    streamRef.current?.close()
    streamRef.current = null
    setLive(null)
  }, [id])
  useEffect(() => {
    const onHide = () => document.hidden && streamRef.current && stop('页面隐藏，已暂停查看；任务继续执行')
    document.addEventListener('visibilitychange', onHide)
    return () => document.removeEventListener('visibilitychange', onHide)
  }, [])

  usePolling(() => void job.reload(), 2500, !streamRef.current && Boolean(job.data && activeStates.includes(job.data.state)))

  function watch() {
    stop()
    const stream = new EventSource(`/v1/tasks/events?id=${encodeURIComponent(id)}`)
    streamRef.current = stream
    setLive({ status: '正在连接…' })
    stream.addEventListener('task', e => {
      try {
        const v = JSON.parse((e as MessageEvent).data)
        if (v.job?.id === id) setLive({ job: v.job, status: `实时状态：${v.job.state}` })
      } catch {
        stop('事件内容无效，请刷新任务')
      }
    })
    stream.addEventListener('done', () => {
      stop('任务已结束；下面是保存的结果')
      void job.reload()
      onChanged()
    })
    stream.addEventListener('reconnect', () => stop('订阅到期；可以重新实时查看'))
    stream.addEventListener('error', e => {
      let message = '连接已断开；任务不会因此取消，可以重新订阅'
      const data = (e as MessageEvent).data
      if (data) {
        try {
          const v = JSON.parse(data)
          message = `${v.error?.code ?? '错误'} · ${v.error?.message ?? message}`
        } catch {
          // 保持默认说明
        }
      }
      stop(message)
    })
  }

  const j = live?.job ?? job.data
  if (!j) return job.error ? <ErrorLine className="m-4">{job.error}</ErrorLine> : null
  const r = (j.result ?? {}) as Record<string, any>
  const evidence: Evidence[] = r.evidence ?? []

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <StateBadge state={j.state} />
        {r.stop_reason && <Badge>停止原因 {r.stop_reason}</Badge>}
        <span className="ml-auto flex flex-wrap gap-1.5">
          {streamRef.current ? (
            <Button size="sm" variant="ghost" onClick={() => stop()}>
              <EyeOff />
              停止查看
            </Button>
          ) : (
            <Button size="sm" variant="ghost" onClick={watch}>
              <Radio />
              实时查看
            </Button>
          )}
          {activeStates.includes(j.state) && (
            <Button
              size="sm"
              variant="danger"
              disabled={j.cancel_requested}
              onClick={async () => {
                if (!(await confirm({ title: '取消这个调查任务？', body: '已经完成的步骤和依据会保留。', confirm: '取消任务', danger: true }))) return
                await run(() => api.cancelJob(j.id), '已请求取消')
                void job.reload()
                onChanged()
              }}
            >
              <Square />
              取消任务
            </Button>
          )}
          {j.state === 'failed' && !j.cancel_requested && (
            <Button
              size="sm"
              variant="primary"
              onClick={async () => {
                await run(() => api.resumeTask(j.id), '原任务已重新入队；预算与截止时间保持不变')
                void job.reload()
                onChanged()
              }}
            >
              <RotateCcw />
              检查条件并继续原任务
            </Button>
          )}
        </span>
      </div>
      {live && <p role="status" aria-live="polite" className="text-xs text-dim">{live.status}</p>}
      {j.error && <ErrorLine>{j.error}</ErrorLine>}
      <KeyValues
        rows={[
          ['任务', <code key="i">{j.id}</code>],
          ['类型', j.kind === 'research' ? '委托调查' : '固定配方'],
          ['尝试', j.attempt],
          ['创建', when(j.created)],
          ['检查点', r.checkpoint ? `${r.checkpoint.revision} · ${r.checkpoint.phase}${r.checkpoint.phase === 'complete' ? ' · 已完成' : ` · 下一步 ${Number(r.checkpoint.next_step) + 1}`}${r.checkpoint.blocking_reason ? ` · ${r.checkpoint.blocking_reason}` : ''}` : '—'],
          ['引用保留', r.references_expire_at ? `至 ${when(r.references_expire_at)}；资料撤回仍然优先` : '未声明长期保留']
        ]}
      />
      {r.unavailable && <ErrorLine>{r.message}</ErrorLine>}
      {Array.isArray(r.findings) && r.findings.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-[13px] font-semibold">发现</h3>
          {r.findings.map((f: { claim: string; evidence_ids: string[] }, i: number) => (
            <div key={i} className="rounded-lg border border-line bg-bg px-3 py-2">
              <p className="text-[13px] text-fg">{f.claim}</p>
              <p className="mt-1 font-mono text-xs text-dim">依据 {f.evidence_ids.join('、')}</p>
            </div>
          ))}
        </div>
      )}
      {Array.isArray(r.unresolved) && r.unresolved.length > 0 && (
        <div>
          <h3 className="text-[13px] font-semibold">未解决</h3>
          <ul className="mt-1 list-disc space-y-0.5 pl-5 text-[13px] text-fg-soft">
            {r.unresolved.map((u: string, i: number) => (
              <li key={i}>{u}</li>
            ))}
          </ul>
        </div>
      )}
      {evidence.length > 0 && (
        <div className="space-y-2">
          <h3 className="text-[13px] font-semibold">类型化依据</h3>
          {evidence.map(e => (
            <details key={e.id} className="rounded-lg border border-line bg-bg">
              <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 px-3 py-2 text-[13px]">
                <code className="text-fg">{e.id}</code>
                <Badge>{e.kind}</Badge>
                {e.metric && (
                  <span className="font-mono text-xs text-dim">
                    {String(e.metric.value)} {e.metric.unit}
                  </span>
                )}
                {e.ref && (
                  <Button
                    size="sm"
                    variant="ghost"
                    className="ml-auto"
                    onClick={ev => {
                      ev.preventDefault()
                      setReading({ ref: e.ref!, kb: j.kb || kb, title: `${e.id} · ${e.kind}` })
                    }}
                  >
                    读取依据
                  </Button>
                )}
              </summary>
              <pre className="max-h-72 overflow-auto border-t px-3 py-2 font-mono text-xs text-fg-soft">{JSON.stringify(e.content, null, 2)}</pre>
            </details>
          ))}
        </div>
      )}
      <JsonBlock value={r} summary="实际步骤与原始结果" />
      <ReaderSheet target={reading} onClose={() => setReading(null)} />
    </div>
  )
}

export default function ResearchPage() {
  const { kb, caps, profile } = useSession()
  const run = useRun()
  const [mode, setMode] = useState<'research' | 'recipe'>(caps.wiki ? 'research' : 'recipe')
  const [goal, setGoal] = useState(kb === 'demo' ? 'E203' : '')
  const [path, setPath] = useState('')
  const [steps, setSteps] = useState(8)
  const [seconds, setSeconds] = useState(120)
  const [recipe, setRecipe] = useState(JSON.stringify(defaultRecipe, null, 2))
  const [selected, setSelected] = useState('')
  const tasks = useData(() => api.jobs({ kb, kind: 'research,recipe' }), [kb])
  usePolling(() => void tasks.reload(), 2500, Boolean(tasks.data?.items.some(t => activeStates.includes(t.state))))
  const items = tasks.data?.items ?? []

  useEffect(() => {
    if (!selected && items[0]) setSelected(items[0].id)
  }, [items, selected])

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    const job = await run(async () => {
      const payload: Record<string, unknown> = { kb, goal, path, max_steps: steps, max_seconds: seconds }
      if (mode === 'recipe') payload.recipe = JSON.parse(recipe)
      return mode === 'recipe' ? api.recipe(payload) : api.research(payload)
    }, '任务已入队；关闭页面不会取消')
    if (job) {
      setSelected(job.id)
      void tasks.reload()
    }
  }

  return (
    <Page>
      <PageHeader
        title="调查"
        desc="委托调查由专用 Agent 按返回结果决定下一步；固定配方按明确步骤运行。两者都只读取选定知识，不自动发布，也不执行业务操作。"
        cli={mode === 'recipe' ? `xrag recipe run --data @recipe-run.json --wait --profile ${profile}` : `xrag research run --data @research-task.json --wait --profile ${profile}`}
      />
      <div className="grid gap-3 lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)] lg:items-start">
        <div className="space-y-3">
          <Panel>
            <PanelHeader title="提交任务" />
            <PanelBody>
              <form onSubmit={submit} className="space-y-3.5">
                <Segmented
                  label="运行方式"
                  value={mode}
                  onChange={setMode}
                  options={[
                    { value: 'research', label: '委托调查 · 使用 Chat', disabled: !caps.wiki, title: caps.wiki ? undefined : '本库未配置 Chat 模型' },
                    { value: 'recipe', label: '固定配方 · 不用规划模型' }
                  ]}
                />
                <Field label={mode === 'recipe' ? '配方输入（替换 $input）' : '调查任务'}>
                  <Textarea required rows={3} value={goal} onChange={e => setGoal(e.target.value)} />
                </Field>
                <Field label="限定目录（可空）">
                  <Input mono value={path} onChange={e => setPath(e.target.value)} placeholder="manual" />
                </Field>
                <div className="grid grid-cols-2 gap-3">
                  <Field label="最多规划步骤">
                    <Input type="number" min={1} max={16} value={steps} onChange={e => setSteps(Number(e.target.value))} />
                  </Field>
                  <Field label="执行时长上限（秒）">
                    <Input type="number" min={1} max={600} value={seconds} onChange={e => setSeconds(Number(e.target.value))} />
                  </Field>
                </div>
                {mode === 'recipe' && (
                  <Field label="配方 JSON" hint="有限的顺序步骤，不是任意流程画布。">
                    <Textarea mono rows={10} value={recipe} onChange={e => setRecipe(e.target.value)} spellCheck={false} />
                  </Field>
                )}
                <Button type="submit" variant="primary">
                  提交任务
                </Button>
                <Note>没有执行器时任务排队。Chat 调用计入知识库预算；默认 8 步、120 秒，不承诺一次查清。失败后可以明确请求继续，原范围、截止时间和已用预算保持不变，结果未知的付费调用不会重放。</Note>
              </form>
            </PanelBody>
          </Panel>
          <Panel>
            <PanelHeader title="本知识库的调查与配方" meta={<span>{items.length} 个</span>} />
            {tasks.data && items.length === 0 && <Empty mark title="还没有提交过调查">可以先用固定配方，在现有资料上试一次。</Empty>}
            <ul>
              {items.map(t => (
                <li key={t.id}>
                  <button
                    type="button"
                    data-job-id={t.id}
                    onClick={() => setSelected(t.id)}
                    className={cn('flex w-full items-center gap-2 border-b px-4 py-2.5 text-left hover:bg-hover', selected === t.id && 'bg-raised shadow-[inset_2px_0_0_var(--color-fg)]')}
                  >
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[13px] text-fg">{String(t.payload?.goal ?? t.id)}</span>
                      <span className="font-mono text-xs text-dim">
                        {t.kind === 'research' ? '委托调查' : '固定配方'} · {tail(t.id, 10)} · {shortTime(t.created)}
                      </span>
                    </span>
                    <StateBadge state={t.state} />
                  </button>
                </li>
              ))}
            </ul>
          </Panel>
        </div>
        <Panel aria-label="过程与依据">
          <PanelHeader title="过程与依据" />
          <PanelBody>{selected ? <TaskDetail id={selected} onChanged={() => void tasks.reload()} /> : <Empty mark={false} title="选择一个任务查看过程与依据" className="px-0" />}</PanelBody>
        </Panel>
      </div>
    </Page>
  )
}
