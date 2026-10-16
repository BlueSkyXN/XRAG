import { useMemo, useState } from 'react'
import { RefreshCw, RotateCcw, Square } from 'lucide-react'
import { api, Job } from '../lib/api'
import { useData, useFeedback, usePolling, useRun } from '../lib/feedback'
import { navigate, useLocation } from '../lib/router'
import { useSession } from '../lib/session'
import { shortTime, tail, when } from '../lib/utils'
import { StateBadge } from '../components/badge'
import { Button } from '../components/button'
import { Segmented } from '../components/chip'
import { JsonBlock } from '../components/code'
import { Dialog, DialogBody, DialogContent, DialogTitle } from '../components/dialog'
import { Page, PageHeader } from '../components/page'
import { Empty, ErrorLine, KeyValues, Loading, Note, Panel, PanelHeader } from '../components/panel'
import { Table, Td, Th } from '../components/table'
import { activeStates, jobKinds } from '../features/jobs'

function summary(job: Job) {
  const r = job.result ?? {}
  if (job.kind === 'build' && r.changed !== undefined) return `变更 ${r.changed} · 未变 ${r.unchanged ?? 0} · 失败 ${r.failed ?? 0}`
  if (job.kind === 'eval' && Array.isArray(r.summaries)) return `${r.summaries.length} 种策略 · Top ${r.k}`
  if ((job.kind === 'research' || job.kind === 'recipe') && r.stop_reason) return `停止原因 ${r.stop_reason}`
  if (job.payload?.goal) return String(job.payload.goal)
  if (job.payload?.topic) return String(job.payload.topic)
  return ''
}

type FileRow = { path: string; state: string; before_edition?: string; after_edition?: string; error?: string; source?: string }

function JobDetail({ id, onClose, onChanged }: { id: string; onClose: () => void; onChanged: () => void }) {
  const run = useRun()
  const { confirm } = useFeedback()
  const job = useData(() => api.job(id), [id])
  const details = useData(() => api.jobDetails(id), [id])
  usePolling(() => void job.reload(), 2500, Boolean(job.data && activeStates.includes(job.data.state)))
  const j = job.data
  const files = (details.data?.files as FileRow[] | undefined) ?? []
  const restOfDetails = details.data ? Object.fromEntries(Object.entries(details.data).filter(([k]) => k !== 'files')) : null

  async function cancel() {
    if (!(await confirm({ title: '请求取消这个任务？', body: '运行中的任务会在下一个安全点停下；已经完成的部分不会回滚。', confirm: '请求取消', danger: true }))) return
    await run(() => api.cancelJob(id), '已请求取消')
    void job.reload()
    onChanged()
  }

  async function resume() {
    await run(() => api.resumeTask(id), '原任务已重新入队；预算与截止时间保持不变')
    void job.reload()
    onChanged()
  }

  return (
    <Dialog open onOpenChange={open => !open && onClose()}>
      <DialogContent side="sheet">
        <DialogTitle>任务 {tail(id, 12)}</DialogTitle>
        <DialogBody className="space-y-4">
          {job.loading && !j && <Loading />}
          <ErrorLine>{job.error}</ErrorLine>
          {j && (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <StateBadge state={j.state} />
                <span className="text-[13px] text-fg-soft">{jobKinds[j.kind] ?? j.kind}</span>
                <span className="ml-auto flex gap-2">
                  {activeStates.includes(j.state) && (
                    <Button size="sm" variant="danger" onClick={() => void cancel()} disabled={j.cancel_requested}>
                      <Square />
                      {j.cancel_requested ? '已请求取消' : '请求取消'}
                    </Button>
                  )}
                  {j.state === 'failed' && !j.cancel_requested && ['research', 'recipe'].includes(j.kind) && (
                    <Button size="sm" variant="primary" onClick={() => void resume()}>
                      <RotateCcw />
                      继续任务
                    </Button>
                  )}
                </span>
              </div>
              {j.state === 'failed' && !j.cancel_requested && ['research', 'recipe'].includes(j.kind) && (
                <Note>继续任务会沿用原范围、截止时间和剩余预算；结果未知的付费调用不会重试。</Note>
              )}
              <KeyValues
                rows={[
                  ['任务', <code key="id">{j.id}</code>],
                  ['知识库', <code key="kb">{j.kb}</code>],
                  ['创建', when(j.created)],
                  ['首次开始', when(j.first_started_at)],
                  ['尝试次数', j.attempt],
                  ['提交者', j.owner ? <code key="o">{j.owner}</code> : '—'],
                  ['摘要', summary(j) || '—']
                ]}
              />
              {j.error && <ErrorLine>{j.error}</ErrorLine>}
              {files.length > 0 && (
                <Panel>
                  <PanelHeader title="逐文件处理" meta={<span>{files.length} 个文件</span>} />
                  <Table>
                    <thead>
                      <tr>
                        <Th>路径</Th>
                        <Th>结果</Th>
                        <Th>版本</Th>
                      </tr>
                    </thead>
                    <tbody>
                      {files.map((f, i) => (
                        <tr key={`${f.path}-${i}`}>
                          <Td className="font-mono text-xs text-fg">
                            {f.path}
                            {f.error && <p className="mt-1 font-sans text-err">{f.error}</p>}
                          </Td>
                          <Td>{f.state === 'published' ? '已发布' : f.state}</Td>
                          <Td className="whitespace-nowrap font-mono text-xs">
                            {f.before_edition ? tail(f.before_edition) : '新文件'} → {f.after_edition ? tail(f.after_edition) : '—'}
                          </Td>
                        </tr>
                      ))}
                    </tbody>
                  </Table>
                </Panel>
              )}
              <JsonBlock value={j.result ?? null} summary="任务结果" />
              <JsonBlock value={j.payload ?? null} summary="提交参数" />
              {restOfDetails && Object.keys(restOfDetails).length > 0 && <JsonBlock value={restOfDetails} summary="模型调用与其他明细" />}
            </>
          )}
        </DialogBody>
      </DialogContent>
    </Dialog>
  )
}

export default function JobsPage() {
  const { kb, profile, system } = useSession()
  const location = useLocation()
  const [scopeKb, setScopeKb] = useState<'current' | 'all'>('current')
  const [kind, setKind] = useState('all')
  const jobs = useData(() => api.jobs(scopeKb === 'current' && kb ? { kb } : {}), [kb, scopeKb])
  const items = jobs.data?.items ?? []
  const active = items.some(j => activeStates.includes(j.state))
  usePolling(() => void jobs.reload(), 2500, active)

  const kinds = useMemo(() => [...new Set(items.map(j => j.kind))], [items])
  const shown = kind === 'all' ? items : items.filter(j => j.kind === kind)
  const openId = location.search.get('id')

  return (
    <Page>
      <PageHeader
        title="任务"
        desc="构建、发布、Wiki、评测和调查都在后台执行。关闭页面不会取消任务。"
        cli={`xrag jobs watch JOB_ID --timeout 10m --profile ${profile}`}
        actions={
          <Button variant="ghost" onClick={() => void jobs.reload()} disabled={jobs.loading}>
            <RefreshCw />
            刷新
          </Button>
        }
      />
      {!system.workers && (
        <ErrorLine className="border-warn/30 bg-warn/[0.06] text-warn">本进程没有启用执行器，任务会排队等待。需要时单独运行 xrag-server worker --data 资料目录。</ErrorLine>
      )}
      <Panel>
        <PanelHeader
          title={`${shown.length} 个任务`}
          meta={active && <span>有任务在进行，每 2.5 秒刷新</span>}
          actions={
            <>
              <Segmented
                label="类型"
                value={kind}
                onChange={setKind}
                options={[{ value: 'all', label: '全部' }, ...kinds.map(k => ({ value: k, label: jobKinds[k] ?? k }))]}
              />
              <Segmented
                label="知识库"
                value={scopeKb}
                onChange={setScopeKb}
                options={[
                  { value: 'current', label: `当前 ${kb}` },
                  { value: 'all', label: '全部' }
                ]}
              />
            </>
          }
        />
        {jobs.loading && !jobs.data && <Loading />}
        <ErrorLine className="m-4">{jobs.error}</ErrorLine>
        {jobs.data && shown.length === 0 && <Empty mark title="当前没有任务">构建、Wiki 生成、评测和调查提交后会出现在这里。</Empty>}
        {shown.length > 0 && (
          <Table>
            <thead>
              <tr>
                <Th>任务</Th>
                <Th>类型</Th>
                <Th>知识库</Th>
                <Th>状态</Th>
                <Th>摘要</Th>
                <Th>尝试</Th>
                <Th>创建</Th>
              </tr>
            </thead>
            <tbody>
              {shown.map(job => (
                <tr
                  key={job.id}
                  tabIndex={0}
                  className="cursor-pointer outline-none hover:bg-hover focus-visible:bg-hover"
                  onClick={() => navigate(`/jobs?id=${job.id}`)}
                  onKeyDown={e => e.key === 'Enter' && navigate(`/jobs?id=${job.id}`)}
                >
                  <Td className="font-mono text-xs text-fg" title={job.id}>
                    {tail(job.id, 12)}
                  </Td>
                  <Td className="whitespace-nowrap">{jobKinds[job.kind] ?? job.kind}</Td>
                  <Td className="font-mono text-xs">{job.kb}</Td>
                  <Td>
                    <StateBadge state={job.state} />
                  </Td>
                  <Td className="max-w-[360px] truncate">
                    {job.error ? <span className="text-err">{job.error}</span> : summary(job) || <span className="text-faint">—</span>}
                  </Td>
                  <Td className="font-mono text-xs">{job.attempt}</Td>
                  <Td className="whitespace-nowrap font-mono text-xs">{shortTime(job.created)}</Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>
      {openId && <JobDetail id={openId} onClose={() => navigate('/jobs', true)} onChanged={() => void jobs.reload()} />}
    </Page>
  )
}
