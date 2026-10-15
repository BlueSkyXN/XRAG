import { SyntheticEvent, useEffect, useState } from 'react'
import { ChevronLeft, ChevronRight, Download } from 'lucide-react'
import { api, downloadJSON, Feedback } from '../lib/api'
import { useData, useRun } from '../lib/feedback'
import { navigate } from '../lib/router'
import { useSession } from '../lib/session'
import { when } from '../lib/utils'
import { Badge, Tone } from '../components/badge'
import { Button } from '../components/button'
import { Segmented } from '../components/chip'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from '../components/dialog'
import { Field, Input, Select, Textarea } from '../components/input'
import { Page, PageHeader } from '../components/page'
import { Empty, ErrorLine, Loading, Note, Panel, PanelHeader } from '../components/panel'
import { feedbackLabels } from '../features/feedback-dialog'
import { ReaderSheet, ReadTarget } from '../features/reader'

const statusInfo: Record<string, { label: string; tone: Tone }> = {
  open: { label: '待处理', tone: 'warn' },
  resolved: { label: '已处理', tone: 'ok' },
  dismissed: { label: '不采纳', tone: 'off' }
}

function Resolution({ item, onSaved }: { item: Feedback; onSaved: () => void }) {
  const run = useRun()
  const [status, setStatus] = useState(item.status)
  const [text, setText] = useState(item.resolution ?? '')
  useEffect(() => {
    setStatus(item.status)
    setText(item.resolution ?? '')
  }, [item])
  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    const ok = await run(() => api.resolveFeedback(item.id, { status, resolution: text, expected_revision: item.revision }), '处理结果已保存')
    if (ok !== undefined) onSaved()
  }
  return (
    <form onSubmit={submit} className="mt-3 grid gap-2 sm:grid-cols-[9rem_minmax(0,1fr)_auto] sm:items-end">
      <Field label="处理结果">
        <Select value={status} onChange={e => setStatus(e.target.value as Feedback['status'])}>
          {Object.entries(statusInfo).map(([v, s]) => (
            <option key={v} value={v}>
              {s.label}
            </option>
          ))}
        </Select>
      </Field>
      <Field label="处理说明">
        <Input maxLength={8000} value={text} onChange={e => setText(e.target.value)} placeholder="已做的调整，或不采纳的原因" />
      </Field>
      <Button type="submit" variant="subtle">
        保存处理
      </Button>
    </form>
  )
}

export default function FeedbackPage() {
  const { kb, may, profile } = useSession()
  const run = useRun()
  const [status, setStatus] = useState('')
  const [offset, setOffset] = useState(0)
  const [reading, setReading] = useState<ReadTarget | null>(null)
  const [caseFor, setCaseFor] = useState<Feedback | null>(null)
  const [question, setQuestion] = useState('')
  const page = useData(() => api.feedback({ kb, status, offset }), [kb, status, offset])

  useEffect(() => setOffset(0), [kb, status])

  async function makeCase(event: SyntheticEvent) {
    event.preventDefault()
    if (!caseFor) return
    const d = await run(() => api.feedbackToCase(caseFor.id, question), '已创建待确认的测试问题')
    if (d) {
      setCaseFor(null)
      navigate('/eval')
    }
  }

  const items = page.data?.items ?? []
  return (
    <Page>
      <PageHeader
        title="反馈"
        desc="查看对检索结果的真实反馈，定位资料并记录处理结果。Contributor 只看到自己的反馈，维护者按知识库查看。"
        cli={`xrag feedback list --profile ${profile}`}
        actions={
          <Button disabled={!page.data} onClick={() => page.data && downloadJSON({ ...page.data, kb, offset, export_scope: 'current displayed page' }, 'feedback-page.json')}>
            <Download />
            导出当前页
          </Button>
        }
      />
      <Panel>
        <PanelHeader
          title={page.data ? `共 ${page.data.total} 条` : '反馈'}
          meta={page.data && <span>本页 {items.length} 条</span>}
          actions={
            <Segmented
              label="处理状态"
              value={status}
              onChange={setStatus}
              options={[{ value: '', label: '全部' }, ...Object.entries(statusInfo).map(([v, s]) => ({ value: v, label: s.label }))]}
            />
          }
        />
        {page.loading && !page.data && <Loading />}
        <ErrorLine className="m-4">{page.error}</ErrorLine>
        {page.data && items.length === 0 && <Empty mark title="当前条件下没有反馈">在检索结果上按 F 或点“反馈这条结果”提交。</Empty>}
        <ul>
          {items.map(f => {
            const st = statusInfo[f.status] ?? { label: f.status, tone: 'neutral' as Tone }
            return (
              <li key={f.id} className="border-b px-4 py-3.5">
                <div className="flex flex-wrap items-center gap-2">
                  <b className="font-semibold">{feedbackLabels[f.label] ?? f.label}</b>
                  <Badge tone={st.tone} dot>
                    {st.label}
                  </Badge>
                  <span className="font-mono text-xs text-faint">
                    {f.id} · {when(f.created)}
                  </span>
                  <span className="ml-auto flex gap-1.5">
                    {f.ref && (
                      <Button size="sm" variant="ghost" onClick={() => setReading({ ref: f.ref!, kb: f.kb, path: f.path })}>
                        读取所引用版本
                      </Button>
                    )}
                    {f.ref && may('maintainer') && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => {
                          setQuestion('')
                          setCaseFor(f)
                        }}
                      >
                        整理成测试问题
                      </Button>
                    )}
                  </span>
                </div>
                <p className="mt-1.5 font-mono text-xs text-dim">{f.path || '来源已撤回或不可用'}</p>
                <p className="mt-1.5 whitespace-pre-wrap text-[13px] text-fg-soft">{f.note || <span className="text-faint">没有填写说明</span>}</p>
                {may('maintainer') ? <Resolution item={f} onSaved={() => void page.reload()} /> : <p className="mt-2 text-xs text-dim">处理说明：{f.resolution || '暂无'}</p>}
              </li>
            )
          })}
        </ul>
        {page.data && (offset > 0 || page.data.next_offset != null) && (
          <div className="flex items-center gap-2 px-4 py-2">
            <Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 20))}>
              <ChevronLeft />
              上一页
            </Button>
            <Button size="sm" variant="ghost" disabled={page.data.next_offset == null} onClick={() => setOffset(page.data!.next_offset ?? offset)}>
              下一页
              <ChevronRight />
            </Button>
          </div>
        )}
      </Panel>

      <ReaderSheet target={reading} onClose={() => setReading(null)} />
      <Dialog open={Boolean(caseFor)} onOpenChange={o => !o && setCaseFor(null)}>
        <DialogContent className="max-w-lg">
          <DialogTitle>从反馈整理测试问题</DialogTitle>
          <form onSubmit={makeCase} className="contents">
            <DialogBody className="space-y-3">
              <p className="font-mono text-xs text-dim">{caseFor?.path}</p>
              <Field label="原始用户问题">
                <Textarea required maxLength={2000} rows={3} value={question} onChange={e => setQuestion(e.target.value)} />
              </Field>
              <Note>期望资料要在“评测”页确认后才能进入题集。</Note>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setCaseFor(null)}>
                取消
              </Button>
              <Button type="submit" variant="primary">
                保存待确认问题
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </Page>
  )
}
