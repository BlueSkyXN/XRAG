import { SyntheticEvent, useState } from 'react'
import { KeyRound, MoreHorizontal } from 'lucide-react'
import { api, ApiKey, Role } from '../lib/api'
import { useData, useFeedback, useRun } from '../lib/feedback'
import { roleLabels, useSession } from '../lib/session'
import { commaList, when } from '../lib/utils'
import { Badge } from '../components/badge'
import { Button } from '../components/button'
import { CodeBlock, CopyButton } from '../components/code'
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogTitle } from '../components/dialog'
import { Field, Input, Select, Textarea } from '../components/input'
import { Menu, MenuContent, MenuItem, MenuTrigger } from '../components/menu'
import { Page, PageHeader } from '../components/page'
import { Empty, KeyValues, Loading, Note, Panel, PanelBody, PanelHeader } from '../components/panel'
import { Table, Td, Th } from '../components/table'

function Keys() {
  const { kb, logout } = useSession()
  const run = useRun()
  const { confirm } = useFeedback()
  const keys = useData(() => api.keys(), [])
  const [form, setForm] = useState({ name: '', role: 'reader' as Role, kbs: kb, expires: '' })
  const [fresh, setFresh] = useState('')
  const [renaming, setRenaming] = useState<ApiKey | null>(null)
  const [newName, setNewName] = useState('')

  async function create(event: SyntheticEvent) {
    event.preventDefault()
    const r = await run(
      () =>
        api.createKey({
          name: form.name.trim(),
          role: form.role,
          kbs: form.role === 'admin' ? [] : commaList(form.kbs),
          expires_at: form.expires ? Math.floor(new Date(form.expires).getTime() / 1000) : 0
        }),
      '已创建。新 Key 只显示这一次，请先保存。'
    )
    if (r) {
      setFresh(r.key)
      setForm({ ...form, name: '' })
      void keys.reload()
    }
  }

  async function disable(k: ApiKey) {
    const current = k.id === keys.data?.current_key_id
    const ok = await confirm({
      title: `停用 Key“${k.name}”？`,
      body: current ? '这是你当前使用的 Key。停用后会立即退出登录。' : '使用这把 Key 的应用和会话，下一次请求就会失效。',
      confirm: '停用',
      danger: true
    })
    if (!ok) return
    const done = await run(() => api.disableKey(k.id), 'Key 已停用')
    if (done === undefined) return
    if (current) await logout()
    else void keys.reload()
  }

  async function rename(event: SyntheticEvent) {
    event.preventDefault()
    if (!renaming) return
    const ok = await run(() => api.renameKey(renaming.id, newName.trim()), '名称已修改；身份和已有引用不变')
    if (ok !== undefined) {
      setRenaming(null)
      void keys.reload()
    }
  }

  return (
    <>
      <div className="grid gap-3 lg:grid-cols-2">
        <Panel>
          <PanelHeader title="创建访问 Key" />
          <PanelBody>
            <form onSubmit={create} className="grid gap-3.5 sm:grid-cols-2">
              <Field label="名称" className="sm:col-span-2">
                <Input required maxLength={120} value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="例如：研发资料 Agent" />
              </Field>
              <Field label="角色">
                <Select value={form.role} onChange={e => setForm({ ...form, role: e.target.value as Role })}>
                  {(Object.keys(roleLabels) as Role[]).map(r => (
                    <option key={r} value={r}>
                      {roleLabels[r]}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="过期时间（留空为不过期）">
                <Input type="datetime-local" value={form.expires} onChange={e => setForm({ ...form, expires: e.target.value })} />
              </Field>
              <Field label="知识库（逗号分隔）" hint={form.role === 'admin' ? 'Admin 对全部知识库生效。' : '显式留空不等于全部；多个知识库请求有一个不允许就整体报错。'} className="sm:col-span-2">
                <Input mono required={form.role !== 'admin'} disabled={form.role === 'admin'} value={form.role === 'admin' ? '' : form.kbs} onChange={e => setForm({ ...form, kbs: e.target.value })} />
              </Field>
              <div className="sm:col-span-2">
                <Button type="submit" variant="primary">
                  <KeyRound />
                  创建并显示一次
                </Button>
              </div>
            </form>
          </PanelBody>
        </Panel>
        <Panel>
          <PanelHeader title="新 Key 只在这里显示一次" />
          <PanelBody className="space-y-3">
            {fresh ? (
              <>
                <Textarea mono readOnly rows={3} value={fresh} onFocus={e => e.currentTarget.select()} />
                <div className="flex gap-2">
                  <CopyButton text={fresh} label="复制新 Key" className="h-8 px-3 text-[13px]" />
                  <Button size="sm" variant="ghost" onClick={() => setFresh('')}>
                    我已保存，隐藏
                  </Button>
                </div>
              </>
            ) : (
              <p className="text-[13px] leading-relaxed text-fg-soft">为不同应用分别创建 Key。更换时先创建新 Key、接入成功后再停用旧的。停用后，现有会话的下一次请求也会失效。</p>
            )}
            <Note>系统不会停用最后一把永久管理员 Key，避免关闭所有管理入口。一把 Key 就是一个调用身份，没有单独的用户密码目录。</Note>
          </PanelBody>
        </Panel>
      </div>
      <Panel>
        <PanelHeader title="访问 Key" meta={<span>{keys.data?.items.length ?? 0} 把</span>} />
        {keys.loading && !keys.data && <Loading />}
        {keys.data && (
          <Table>
            <thead>
              <tr>
                <Th>名称 · 身份</Th>
                <Th>角色 · 范围</Th>
                <Th>有效期</Th>
                <Th>状态</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {keys.data.items.map(k => (
                <tr key={k.id}>
                  <Td>
                    <span className="text-fg">{k.name}</span>
                    {k.id === keys.data!.current_key_id && (
                      <Badge tone="ok" className="ml-2">
                        当前
                      </Badge>
                    )}
                    <code className="mt-0.5 block text-xs text-dim">{k.id}</code>
                  </Td>
                  <Td>
                    {roleLabels[k.role] ?? k.role}
                    <span className="mt-0.5 block font-mono text-xs text-dim">{k.role === 'admin' ? '全部知识库' : k.kbs.join(', ') || '无'}</span>
                  </Td>
                  <Td className="whitespace-nowrap text-xs">{when(k.expires_at, '不过期')}</Td>
                  <Td>
                    <Badge tone={k.disabled ? 'off' : k.state === 'active' ? 'ok' : 'warn'} dot>
                      {k.disabled ? '已停用' : k.state === 'active' ? '可用' : k.state}
                    </Badge>
                  </Td>
                  <Td className="text-right">
                    <Menu>
                      <MenuTrigger asChild>
                        <Button size="icon-sm" variant="ghost" aria-label={`${k.name} 的操作`}>
                          <MoreHorizontal />
                        </Button>
                      </MenuTrigger>
                      <MenuContent>
                        <MenuItem
                          onSelect={() => {
                            setNewName(k.name)
                            setRenaming(k)
                          }}
                        >
                          改名
                        </MenuItem>
                        <MenuItem className="text-err" disabled={k.disabled} onSelect={() => void disable(k)}>
                          停用
                        </MenuItem>
                      </MenuContent>
                    </Menu>
                  </Td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </Panel>
      <Dialog open={Boolean(renaming)} onOpenChange={o => !o && setRenaming(null)}>
        <DialogContent className="max-w-md">
          <DialogTitle>修改 Key 名称</DialogTitle>
          <form onSubmit={rename} className="contents">
            <DialogBody>
              <Field label="新的显示名称" hint="不改变身份和已有引用。">
                <Input required maxLength={120} value={newName} onChange={e => setNewName(e.target.value)} />
              </Field>
            </DialogBody>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setRenaming(null)}>
                取消
              </Button>
              <Button type="submit" variant="primary">
                保存
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}

export default function AccessPage() {
  const { system, kb, profile, setProfile, may } = useSession()
  const [draft, setDraft] = useState(profile)
  const origin = window.location.origin
  const mcp = system.mcp
  return (
    <Page>
      <PageHeader title="接入" desc="把同一组检索能力交给 CLI、MCP 客户端和你的应用。浏览器登录只用于网页；程序调用使用 API Key。" cli={`xrag keys list --profile ${profile}`} />
      <div className="grid gap-3 lg:grid-cols-2">
        <Panel>
          <PanelHeader title="Client 连接" />
          <PanelBody className="space-y-3">
            <form
              className="flex items-end gap-2"
              onSubmit={e => {
                e.preventDefault()
                if (draft.trim()) setProfile(draft.trim())
              }}
            >
              <Field label="CLI profile 名称" hint="只保存在这个浏览器里，用来生成页面上的 CLI 示例。" className="flex-1">
                <Input mono value={draft} onChange={e => setDraft(e.target.value)} />
              </Field>
              <Button type="submit" disabled={!draft.trim() || draft.trim() === profile}>
                保存
              </Button>
            </form>
            <CodeBlock
              label="登记 profile"
              meta="Key 文件只放在本机"
              code={`xrag profile add ${profile} \\\n  --server ${origin} \\\n  --key-file /本机/保存的/key --kb ${kb}`}
            />
            <CodeBlock label="使用" code={`xrag +ask "日志丢失" --profile ${profile}\nxrag search keyword E203 --profile ${profile}\nxrag overview --profile ${profile}`} />
            <Note>Client local 模式只读扫描本地目录、不建索引；direct 模式直连已有后端，由后端自己鉴权。</Note>
          </PanelBody>
        </Panel>
        <Panel aria-label="MCP">
          <PanelHeader title="MCP" meta={<span>{mcp?.enabled ? '已启用' : '本进程未启用'}</span>} />
          <PanelBody className="space-y-3">
            <KeyValues
              rows={[
                ['HTTP 端点', <code key="e">{origin}/mcp</code>],
                ['传输', `POST ${(mcp?.http_post ?? []).map(t => t.toUpperCase()).join(' / ')}；CLI 另支持 stdio`],
                ['协议版本', (mcp?.versions ?? []).join('、') || '—'],
                ['资源与提示', mcp?.resources ? '检索指南、可见目录、固定引用；提示教 Agent 组合查法' : '—'],
                ['外部 OAuth', mcp?.oauth_resource_server ? '已启用资源服务（令牌检查）' : '未启用；默认使用 API Key']
              ]}
            />
            <CodeBlock label="stdio" code={`xrag mcp --profile ${profile} --toolsets core,graph,files,utility,research`} />
            <Note>只承诺上面列出的协议修订，不宣称支持 2026 新协议的全部行为；没有自建授权登录服务。使用指南随源码提供：skills/xrag-retrieval/SKILL.md。</Note>
          </PanelBody>
        </Panel>
      </div>
      {may('admin') ? (
        <Keys />
      ) : (
        <Panel>
          <Empty title="访问 Key 由管理员创建和管理">需要新的程序身份时，请管理员为你创建一把限定知识库和角色的 Key。</Empty>
        </Panel>
      )}
    </Page>
  )
}
