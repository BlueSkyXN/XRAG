import { createContext, ReactNode, useCallback, useContext, useEffect, useRef, useState } from 'react'
import { ApiError } from './api'
import { cn } from './utils'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from '../components/dialog'
import { Button } from '../components/button'

type Tone = 'info' | 'ok' | 'err'
type Toast = { id: number; tone: Tone; text: string }
type ConfirmOptions = { title: string; body?: ReactNode; confirm?: string; danger?: boolean; requireText?: string }

type FeedbackValue = {
  toast: (text: string, tone?: Tone) => void
  confirm: (options: ConfirmOptions) => Promise<boolean>
}

const FeedbackContext = createContext<FeedbackValue | null>(null)
const ExpireContext = createContext<() => void>(() => undefined)

export function FeedbackProvider({ children, onExpire }: { children: ReactNode; onExpire: () => void }) {
  const [toasts, setToasts] = useState<Toast[]>([])
  const [pending, setPending] = useState<(ConfirmOptions & { resolve: (ok: boolean) => void }) | null>(null)
  const [typed, setTyped] = useState('')
  const seq = useRef(0)

  const toast = useCallback((text: string, tone: Tone = 'info') => {
    const id = ++seq.current
    setToasts(list => [...list.slice(-3), { id, tone, text }])
    setTimeout(() => setToasts(list => list.filter(t => t.id !== id)), tone === 'err' ? 7000 : 4000)
  }, [])

  const confirm = useCallback((options: ConfirmOptions) => {
    setTyped('')
    return new Promise<boolean>(resolve => setPending({ ...options, resolve }))
  }, [])

  function close(ok: boolean) {
    pending?.resolve(ok)
    setPending(null)
  }

  const blocked = Boolean(pending?.requireText) && typed !== pending?.requireText

  return (
    <FeedbackContext.Provider value={{ toast, confirm }}>
      <ExpireContext.Provider value={onExpire}>
        {children}
        <div className="pointer-events-none fixed bottom-4 right-4 z-[60] flex w-[min(420px,calc(100vw-2rem))] flex-col gap-2" role="status" aria-live="polite">
          {toasts.map(t => (
            <div
              key={t.id}
              className={cn(
                'pointer-events-auto animate-fade-in rounded-lg border bg-raised px-3.5 py-2.5 text-[13px] shadow-xl shadow-shadow',
                t.tone === 'err' ? 'border-err/50 text-fg' : t.tone === 'ok' ? 'border-ok/40 text-fg' : 'border-line-strong text-fg'
              )}
            >
              <span className={cn('mr-2 inline-block h-1.5 w-1.5 rounded-full align-middle', t.tone === 'err' ? 'bg-err' : t.tone === 'ok' ? 'bg-ok' : 'bg-dim')} />
              {t.text}
            </div>
          ))}
        </div>
        <Dialog open={Boolean(pending)} onOpenChange={open => !open && close(false)}>
          <DialogContent className="max-w-md">
            <DialogTitle>{pending?.title}</DialogTitle>
            <DialogBody className="space-y-3 text-[13px] text-fg-soft">
              {pending?.body}
              {pending?.requireText && (
                <label className="block space-y-1.5">
                  <span className="text-xs text-dim">
                    输入 <code className="text-fg">{pending.requireText}</code> 确认
                  </span>
                  <input
                    autoFocus
                    value={typed}
                    onChange={event => setTyped(event.target.value)}
                    className="h-8 w-full rounded-md border border-line-strong bg-field px-2.5 font-mono text-[13px] outline-none focus:border-dim"
                  />
                </label>
              )}
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={() => close(false)}>
                取消
              </Button>
              <Button variant={pending?.danger ? 'danger' : 'primary'} disabled={blocked} onClick={() => close(true)}>
                {pending?.confirm ?? '确认'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </ExpireContext.Provider>
    </FeedbackContext.Provider>
  )
}

export function useFeedback() {
  const value = useContext(FeedbackContext)
  if (!value) throw new Error('FeedbackProvider missing')
  return value
}

// 只映射能由错误码确定含义的错误；通用码保留服务端的具体原因。
const errorMessages: Record<string, string> = {
  LAST_ADMIN: '不能停用最后一把永久管理员 Key，请先创建另一把永久管理员 Key',
  CONFIG_EXTERNAL: '服务使用了 --config 配置文件，请修改该文件，或移除 --config 并重启服务后使用网页设置',
  CANDIDATE_LIMIT: '关键词候选数量已达上限，请缩小知识库或文件范围后重试',
  EXPORT_LIMIT: '导出最多支持 64 MiB、10000 个已发布文件，请缩小知识库后导出',
  IDEMPOTENCY_CONFLICT: '同一幂等键对应了不同请求，请核对原请求',
  JOB_EXPIRED: '任务已结束，其详细记录已清理',
  ORIGIN_MISMATCH: '这条反馈属于其他数据来源，请切换到原服务后查看',
  EVAL_DRAFT_UNAPPROVED: '请先审核确认预期结果，再发布题集',
  EVAL_SPLIT_OVERLAP: '同一题集系列的开发集和留出集包含重复问题，请移除重复题目',
  CHECKPOINT_MISSING: '任务没有可用于续跑的检查点，请重新提交任务',
  TASK_CONFIGURATION_CHANGED: '任务配置已改变，请恢复原配置或重新提交任务',
  TASK_DEADLINE_EXCEEDED: '原任务的执行期限已过，请重新提交任务'
}

export function errorText(error: unknown) {
  if (error instanceof ApiError) {
    if (Object.hasOwn(errorMessages, error.code)) return `${errorMessages[error.code]}（${error.code}）`
    return error.code && !/^\d+$/.test(error.code) ? `${error.code} · ${error.message}` : error.message
  }
  return error instanceof Error ? error.message : String(error)
}

/** 执行一次操作：失败时提示原因，会话过期时回到登录页。返回 undefined 表示失败。 */
export function useRun() {
  const { toast } = useFeedback()
  const expire = useContext(ExpireContext)
  return useCallback(
    async <T,>(fn: () => Promise<T>, success?: string): Promise<T | undefined> => {
      try {
        const value = await fn()
        if (success) toast(success, 'ok')
        return value
      } catch (error) {
        if (error instanceof ApiError && error.status === 401) {
          expire()
          return undefined
        }
        toast(errorText(error), 'err')
        return undefined
      }
    },
    [expire, toast]
  )
}

type DataState<T> = { data: T | undefined; error: string; loading: boolean }

/** 读取页面数据。依赖变化时重新读取，旧请求的结果直接丢弃。 */
export function useData<T>(load: (() => Promise<T>) | null, deps: unknown[]) {
  const expire = useContext(ExpireContext)
  const [state, setState] = useState<DataState<T>>({ data: undefined, error: '', loading: Boolean(load) })
  const seq = useRef(0)
  const loader = useRef(load)
  loader.current = load

  const reload = useCallback(async () => {
    const fn = loader.current
    if (!fn) {
      setState({ data: undefined, error: '', loading: false })
      return
    }
    const id = ++seq.current
    setState(s => ({ ...s, loading: true, error: '' }))
    try {
      const data = await fn()
      if (id === seq.current) setState({ data, error: '', loading: false })
    } catch (error) {
      if (id !== seq.current) return
      if (error instanceof ApiError && error.status === 401) {
        expire()
        return
      }
      setState(s => ({ ...s, error: errorText(error), loading: false }))
    }
  }, [expire])

  useEffect(() => {
    void reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps)

  const setData = useCallback((data: T) => setState(s => ({ ...s, data })), [])
  return { ...state, reload, setData }
}

/** 页面可见时按固定间隔刷新；页面隐藏时暂停。 */
export function usePolling(fn: () => void, ms: number, active = true) {
  const ref = useRef(fn)
  ref.current = fn
  useEffect(() => {
    if (!active) return
    const timer = setInterval(() => {
      if (!document.hidden) ref.current()
    }, ms)
    return () => clearInterval(timer)
  }, [ms, active])
}
