import { SyntheticEvent, useEffect, useState } from 'react'
import { api } from '../lib/api'
import { useRun } from '../lib/feedback'
import { Button } from '../components/button'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from '../components/dialog'
import { Field, Select, Textarea } from '../components/input'

export const feedbackLabels: Record<string, string> = {
  useful: '有用',
  irrelevant: '不相关',
  wrong: '内容有误',
  outdated: '内容过时'
}

type Target = { kb: string; ref: string; label: string }

/** 对一条结果提交反馈。一条反馈只评价这一个结果。 */
export function FeedbackDialog({ open, onOpenChange, target }: { open: boolean; onOpenChange: (open: boolean) => void; target: Target | null }) {
  const run = useRun()
  const [label, setLabel] = useState('useful')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (open) {
      setLabel('useful')
      setNote('')
    }
  }, [open])

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    if (!target) return
    setBusy(true)
    const ok = await run(() => api.sendFeedback({ kb: target.kb, ref: target.ref, label, note }), '反馈已保存，可在“反馈”页查看处理进度')
    setBusy(false)
    if (ok !== undefined) onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogTitle>反馈这条结果</DialogTitle>
        <form onSubmit={submit} className="contents">
          <DialogBody className="space-y-4">
            <p className="break-all font-mono text-xs text-dim">{target?.label}</p>
            <Field label="反馈类型">
              <Select value={label} onChange={e => setLabel(e.target.value)}>
                {Object.entries(feedbackLabels).map(([value, text]) => (
                  <option key={value} value={value}>
                    {text}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="具体说明" hint="例如缺少哪个版本范围、原本期望找到哪份资料。">
              <Textarea rows={4} maxLength={8000} value={note} onChange={e => setNote(e.target.value)} />
            </Field>
          </DialogBody>
          <DialogFooter>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button type="submit" variant="primary" disabled={busy}>
              保存反馈
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
