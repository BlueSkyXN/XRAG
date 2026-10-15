import { KeyboardEvent, useEffect, useMemo, useRef, useState } from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { Kbd } from '../components/badge'
import { navigate } from '../lib/router'
import { useSession } from '../lib/session'
import { useTheme, type ThemePreference } from '../lib/theme'
import { cn } from '../lib/utils'
import { pageForAction, pages } from './nav'

type Entry = {
  key: string
  group: string
  name: string
  desc: string
  tag?: string
  disabled?: string
  run: () => void
}

const groupNames: Record<string, string> = {
  core: '查询动作 · core',
  files: '原文 · files',
  graph: '关系 · graph',
  utility: '范围 · utility',
  external: '外部 · external'
}

export function Palette({
  open,
  onOpenChange,
  sidebarExpanded,
  onToggleSidebar
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  sidebarExpanded: boolean
  onToggleSidebar: () => void
}) {
  const { actions, kbs, kb, setKb, may, caps } = useSession()
  const { preference, setPreference } = useTheme()
  const [text, setText] = useState('')
  const [active, setActive] = useState(0)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (open) {
      setText('')
      setActive(0)
    }
  }, [open])

  const entries = useMemo<Entry[]>(() => {
    const unavailable = (action: string) => {
      if (action === 'search.semantic' && !caps.embedding) return '未配置向量模型'
      if (action === 'search.external' && !caps.external) return '未登记外部检索服务'
      return undefined
    }
    const themeEntries: Entry[] = (['system', 'dark', 'light', 'light-color'] as ThemePreference[]).map(value => {
      const labels: Record<ThemePreference, string> = {
        system: '跟随系统',
        dark: '深色',
        light: '浅色',
        'light-color': '浅色多彩'
      }
      return {
        key: `theme:${value}`,
        group: '外观',
        name: `主题：${labels[value]}`,
        desc: '切换工作台主题',
        tag: value === preference ? '当前' : undefined,
        run: () => setPreference(value)
      }
    })
    const sidebarEntry: Entry = {
      key: 'sidebar:toggle',
      group: '外观',
      name: sidebarExpanded ? '收起侧栏' : '展开侧栏',
      desc: '切换桌面侧栏宽度',
      tag: '⌘B',
      run: onToggleSidebar
    }
    // 外观条目放在最后：打开面板时默认高亮的仍是第一个动作。
    const list: Entry[] = [
      ...actions.map(a => ({
        key: `action:${a.action}`,
        group: groupNames[a.group] ?? a.group,
        name: a.cli,
        desc: a.description,
        tag: a.group,
        disabled: unavailable(a.action),
        run: () => navigate(pageForAction(a.action))
      })),
      ...pages
        .filter(p => may(p.min))
        .map(p => ({ key: `page:${p.id}`, group: '页面', name: p.label, desc: p.hint, tag: p.group, run: () => navigate(`/${p.id}`) })),
      ...kbs.map(k => ({
        key: `kb:${k.id}`,
        group: '切换知识库',
        name: k.id,
        desc: k.name + (k.id === kb ? ' · 当前' : ''),
        tag: k.files !== undefined ? `${k.files} 文件` : undefined,
        run: () => setKb(k.id)
      })),
      sidebarEntry,
      ...themeEntries
    ]
    const needle = text.trim().toLowerCase()
    return needle ? list.filter(e => `${e.name} ${e.desc} ${e.tag ?? ''}`.toLowerCase().includes(needle)) : list
  }, [actions, pages, kbs, kb, text, caps, may, setKb, preference, setPreference, sidebarExpanded, onToggleSidebar])

  useEffect(() => {
    listRef.current?.querySelector(`[data-index="${active}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [active])

  function choose(entry: Entry | undefined) {
    if (!entry || entry.disabled) return
    onOpenChange(false)
    entry.run()
  }

  function onKeyDown(event: KeyboardEvent) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault()
      const step = event.key === 'ArrowDown' ? 1 : -1
      setActive(i => Math.min(entries.length - 1, Math.max(0, i + step)))
    } else if (event.key === 'Enter') {
      event.preventDefault()
      choose(entries[active])
    }
  }

  let lastGroup = ''
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-scrim" />
        <DialogPrimitive.Content
          aria-describedby={undefined}
          className="fixed left-1/2 top-[12vh] z-50 w-[min(660px,calc(100vw-2rem))] -translate-x-1/2 overflow-hidden rounded-xl border border-line-strong bg-panel shadow-2xl shadow-shadow animate-fade-in outline-none"
        >
          <DialogPrimitive.Title className="sr-only">命令面板</DialogPrimitive.Title>
          <input
            autoFocus
            value={text}
            onChange={event => {
              setText(event.target.value)
              setActive(0)
            }}
            onKeyDown={onKeyDown}
            placeholder="输入动作、页面或知识库…"
            aria-label="搜索命令"
            role="combobox"
            aria-expanded="true"
            aria-controls="palette-list"
            aria-activedescendant={entries[active] ? `palette-${active}` : undefined}
            className="h-12 w-full border-b bg-transparent px-4 text-[15px] outline-none placeholder:text-faint"
          />
          <div ref={listRef} id="palette-list" role="listbox" className="max-h-[56vh] overflow-y-auto py-1.5">
            {entries.length === 0 && <p className="px-4 py-6 text-[13px] text-dim">没有匹配的动作、页面或知识库。</p>}
            {entries.map((entry, index) => {
              const header = entry.group !== lastGroup ? entry.group : ''
              lastGroup = entry.group
              return (
                <div key={entry.key}>
                  {header && <p className="px-4 pb-1 pt-2.5 text-[11.5px] text-faint">{header}</p>}
                  <div
                    id={`palette-${index}`}
                    data-index={index}
                    role="option"
                    aria-selected={index === active}
                    aria-disabled={Boolean(entry.disabled)}
                    onMouseMove={() => setActive(index)}
                    onClick={() => choose(entry)}
                    className={cn(
                      'grid cursor-pointer grid-cols-[minmax(9rem,auto)_minmax(0,1fr)_auto] items-center gap-3 px-4 py-1.5 text-[13px]',
                      index === active && 'bg-raised shadow-[inset_2px_0_0_var(--color-fg)]',
                      entry.disabled && 'cursor-not-allowed opacity-50'
                    )}
                  >
                    <code className="truncate text-[12.5px] text-fg">{entry.name}</code>
                    <span className="truncate text-fg-soft">{entry.disabled ? `${entry.desc} · ${entry.disabled}` : entry.desc}</span>
                    {entry.tag && <span className="font-mono text-[11.5px] text-faint">{entry.tag}</span>}
                  </div>
                </div>
              )
            })}
          </div>
          <footer className="flex gap-4 border-t px-4 py-2 text-xs text-faint">
            <span className="flex items-center gap-1">
              <Kbd>↑</Kbd>
              <Kbd>↓</Kbd> 选择
            </span>
            <span className="flex items-center gap-1">
              <Kbd>↵</Kbd> 打开
            </span>
            <span className="flex items-center gap-1">
              <Kbd>Esc</Kbd> 关闭
            </span>
          </footer>
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
