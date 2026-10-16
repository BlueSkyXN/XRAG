import { SyntheticEvent, useEffect, useState } from 'react'
import { ArrowRight } from 'lucide-react'
import { Button } from '../components/button'
import { Field, Input } from '../components/input'
import { ErrorLine } from '../components/panel'
import { api } from '../lib/api'
import { errorText } from '../lib/feedback'

export function Login({ onDone }: { onDone: () => Promise<void> }) {
  const [mode, setMode] = useState<'key' | 'password' | null>(null)
  const [version, setVersion] = useState('')
  const [username, setUsername] = useState('')
  const [secret, setSecret] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    api.loginMode().then(m => setMode(m.mode), () => setMode('password'))
    fetch('/healthz')
      .then(r => r.json())
      .then(h => setVersion(h.version ?? ''))
      .catch(() => undefined)
  }, [])

  async function submit(event: SyntheticEvent) {
    event.preventDefault()
    setError('')
    setBusy(true)
    try {
      await api.login(mode === 'password' ? { username: username.trim(), password: secret } : { key: secret.trim() })
      setSecret('')
      await onDone()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <main className="grid min-h-dvh place-items-center px-4">
      <div className="w-full max-w-[380px]">
        <div className="mb-8 flex items-center gap-3">
          <span className="grid h-9 w-9 place-items-center rounded-[9px] bg-fg font-mono text-[16px] font-extrabold text-bg">X</span>
          <div>
            <p className="text-[15px] font-semibold">XRAG</p>
            <p className="font-mono text-xs text-faint">{version || ' '}</p>
          </div>
        </div>
        <h1 className="text-[20px] font-semibold leading-snug tracking-tight">可组合的多路知识检索</h1>
        <p className="mt-1.5 text-[13px] leading-relaxed text-dim">检索资料，查看原文、行号和版本。</p>

        <form id={mode === 'key' ? 'key-form' : 'password-form'} onSubmit={submit} className="mt-7 space-y-3.5 rounded-xl border border-line bg-panel p-5">
          {mode === 'key' ? (
            <Field label="API Key">
              <Input type="password" mono value={secret} onChange={e => setSecret(e.target.value)} placeholder="xrag_…" autoComplete="off" required autoFocus />
            </Field>
          ) : (
            <>
              <Field label="用户名">
                <Input value={username} onChange={e => setUsername(e.target.value)} autoComplete="username" required autoFocus disabled={!mode} />
              </Field>
              <Field label="密码">
                <Input type="password" value={secret} onChange={e => setSecret(e.target.value)} autoComplete="current-password" required disabled={!mode} />
              </Field>
            </>
          )}
          <ErrorLine>{error}</ErrorLine>
          <Button type="submit" variant="primary" size="lg" className="w-full" disabled={busy || !mode}>
            {busy ? '正在登录…' : '进入工作台'}
            {!busy && <ArrowRight />}
          </Button>
        </form>
      </div>
    </main>
  )
}
