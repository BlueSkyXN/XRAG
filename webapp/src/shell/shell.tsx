import { ReactNode, useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Check, ChevronDown, Command, LogOut, Menu as MenuIcon, Monitor, Moon, Palette as PaletteIcon, PanelLeftClose, PanelLeftOpen, Plus, RefreshCw, Sun } from 'lucide-react'
import { Button } from '../components/button'
import { Dot, Kbd } from '../components/badge'
import { Dialog, DialogContent, DialogTitle } from '../components/dialog'
import { Menu, MenuContent, MenuItem, MenuLabel, MenuSeparator, MenuTrigger } from '../components/menu'
import { linkHandler, navigate } from '../lib/router'
import { roleLabels, useSession } from '../lib/session'
import { useRun } from '../lib/feedback'
import { useTheme, type ThemePreference } from '../lib/theme'
import { cn } from '../lib/utils'
import { CurrentPageContext, PageDef, PageId, pages } from './nav'
import { Palette } from './palette'

function useVisiblePages() {
  const { may } = useSession()
  return pages.filter(p => may(p.min))
}

function NavIcon({ page, className }: { page: PageDef; className?: string }) {
  const Icon = page.icon
  return (
    <span
      aria-hidden="true"
      className={cn('nav-icon shrink-0', className)}
      data-icon-tone={page.id === 'sources' ? 'dark' : 'light'}
      style={{ '--hue': `var(--hue-${page.id})` } as CSSProperties}
    >
      <Icon className="h-4 w-4" strokeWidth={1.6} />
    </span>
  )
}

function Rail({ current, expanded, onToggle }: { current: PageId; expanded: boolean; onToggle: () => void }) {
  const visible = useVisiblePages()
  const groups = [...new Set(visible.map(p => p.group))]
  return (
    <nav aria-label="工作台" className={cn('hidden shrink-0 flex-col border-r py-2.5 transition-[width] md:flex', expanded ? 'w-[208px]' : 'w-[60px] items-center gap-0.5')}>
      {groups.map((group, groupIndex) => (
        <div key={group} className={expanded ? cn('w-full', group === '系统' && 'mt-auto') : 'contents'}>
          {expanded ? (
            <p className="px-3 pb-1 pt-2 text-[11.5px] text-faint">{group}</p>
          ) : (
            <>
              {groupIndex > 0 && group !== '系统' && <hr className="my-1.5 w-6 border-line" />}
              {group === '系统' && <span className="flex-1" />}
            </>
          )}
          <div className={expanded ? 'flex w-full flex-col gap-0.5 px-2' : 'contents'}>
            {visible
              .filter(p => p.group === group)
              .map((page: PageDef) => (
                <a
                  key={page.id}
                  href={`/${page.id}`}
                  onClick={linkHandler(`/${page.id}`)}
                  aria-label={page.label}
                  aria-current={current === page.id ? 'page' : undefined}
                  className={cn(
                    'text-faint transition-colors hover:bg-raised hover:text-fg',
                    expanded ? 'flex h-8 items-center gap-2.5 overflow-hidden rounded-md px-1.5 text-[13px] text-fg-soft' : 'group relative grid h-10 w-11 place-items-center rounded-lg',
                    current === page.id && 'bg-raised text-fg shadow-[inset_0_0_0_1px_var(--color-line-strong)]'
                  )}
                >
                  <NavIcon page={page} />
                  <span className={expanded ? 'truncate' : 'sr-only'}>{page.label}</span>
                  {!expanded && (
                    <span className="pointer-events-none absolute left-[52px] z-30 hidden whitespace-nowrap rounded-md border border-line-strong bg-raised px-2 py-1 text-xs text-fg shadow-lg group-hover:block group-focus-visible:block">
                      {page.label}
                    </span>
                  )}
                </a>
              ))}
          </div>
        </div>
      ))}
      <div className={cn('shrink-0', expanded && 'w-full px-2')}>
        <hr className={cn('border-line', expanded ? 'mb-1' : 'my-1.5 w-6')} />
        <Button
          variant="ghost"
          size={expanded ? 'sm' : 'icon'}
          className={expanded ? 'w-full justify-start' : 'group relative h-10 w-11'}
          aria-expanded={expanded}
          aria-label={expanded ? '收起侧栏' : '展开侧栏'}
          onClick={onToggle}
        >
          {expanded ? (
            <>
              <PanelLeftClose />
              <span className="truncate">收起侧栏</span>
              <Kbd className="ml-auto">⌘B</Kbd>
            </>
          ) : (
            <>
              <PanelLeftOpen />
              <span className="pointer-events-none absolute left-[52px] z-30 hidden whitespace-nowrap rounded-md border border-line-strong bg-raised px-2 py-1 text-xs text-fg shadow-lg group-hover:block group-focus-visible:block">
                展开侧栏 ⌘B
              </span>
            </>
          )}
        </Button>
      </div>
    </nav>
  )
}

function MobileNav({ current, open, onOpenChange }: { current: PageId; open: boolean; onOpenChange: (open: boolean) => void }) {
  const visible = useVisiblePages()
  const groups = [...new Set(visible.map(p => p.group))]
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent side="left">
        <DialogTitle>XRAG</DialogTitle>
        <nav aria-label="工作台" className="flex-1 overflow-y-auto p-2">
          {groups.map(group => (
            <div key={group} className="mb-3">
              <p className="px-2 pb-1 text-[11.5px] text-faint">{group}</p>
              {visible
                .filter(p => p.group === group)
                .map((page: PageDef) => (
                  <a
                    key={page.id}
                    href={`/${page.id}`}
                    onClick={event => {
                      linkHandler(`/${page.id}`)(event)
                      onOpenChange(false)
                    }}
                    aria-current={current === page.id ? 'page' : undefined}
                    className={cn('flex h-9 items-center gap-2.5 rounded-md px-2 text-[13px] text-fg-soft hover:bg-raised', current === page.id && 'bg-raised text-fg')}
                  >
                    <NavIcon page={page} />
                    {page.label}
                  </a>
                ))}
            </div>
          ))}
        </nav>
      </DialogContent>
    </Dialog>
  )
}

function ThemeSwitcher() {
  const { preference, system, setPreference } = useTheme()
  const options: { value: ThemePreference; label: string; icon: typeof Monitor }[] = [
    { value: 'system', label: '跟随系统', icon: Monitor },
    { value: 'dark', label: '深色', icon: Moon },
    { value: 'light', label: '浅色', icon: Sun },
    { value: 'light-color', label: '浅色多彩', icon: PaletteIcon }
  ]
  const ActiveIcon = options.find(option => option.value === preference)?.icon ?? Monitor
  return (
    <Menu>
      <MenuTrigger asChild>
        <Button variant="ghost" size="icon-sm" aria-label="主题">
          <ActiveIcon />
        </Button>
      </MenuTrigger>
      <MenuContent align="end">
        <MenuLabel>主题</MenuLabel>
        {options.map(option => (
          <MenuItem key={option.value} onSelect={() => setPreference(option.value)}>
            <Check className={cn(option.value === preference ? 'opacity-100' : 'opacity-0')} />
            {option.label}
            {option.value === 'system' && <span className="ml-auto pl-3 text-xs text-faint">系统{system === 'dark' ? '深色' : '浅色'}</span>}
          </MenuItem>
        ))}
      </MenuContent>
    </Menu>
  )
}

function KbSwitcher() {
  const { kbs, kb, kbName, setKb, may } = useSession()
  return (
    <Menu>
      <MenuTrigger asChild>
        <button
          type="button"
          className="inline-flex h-7 min-w-0 max-w-[260px] items-center gap-2 rounded-md border border-line-strong bg-panel px-2.5 text-[12.5px] hover:bg-raised"
          aria-label="切换知识库"
        >
          {kb ? (
            <>
              <code className="truncate text-fg">{kb}</code>
              <span className="hidden truncate text-dim sm:inline">{kbName}</span>
            </>
          ) : (
            <span className="text-dim">没有知识库</span>
          )}
          <ChevronDown className="h-3.5 w-3.5 shrink-0 text-faint" />
        </button>
      </MenuTrigger>
      <MenuContent align="start">
        <MenuLabel>知识库</MenuLabel>
        {kbs.map(item => (
          <MenuItem key={item.id} onSelect={() => setKb(item.id)}>
            <Check className={cn(item.id === kb ? 'opacity-100' : 'opacity-0')} />
            <code className="text-[12.5px]">{item.id}</code>
            <span className="truncate text-dim">{item.name}</span>
            {item.files !== undefined && <span className="ml-auto pl-3 font-mono text-[11.5px] text-faint">{item.files} 文件</span>}
          </MenuItem>
        ))}
        {may('admin') && (
          <>
            <MenuSeparator />
            <MenuItem onSelect={() => navigate('/overview?create=1')}>
              <Plus />
              新建知识库
            </MenuItem>
          </>
        )}
      </MenuContent>
    </Menu>
  )
}

function TopBar({ onMenu, onPalette }: { onMenu: () => void; onPalette: () => void }) {
  const { system, logout, refresh, profile } = useSession()
  const run = useRun()
  return (
    <header className="flex h-12 shrink-0 items-center gap-3 border-b px-3">
      <Button variant="ghost" size="icon" className="md:hidden" onClick={onMenu} aria-label="打开导航">
        <MenuIcon />
      </Button>
      <a href="/search" onClick={linkHandler('/search')} className="flex items-center gap-2" aria-label="XRAG 首页">
        <span className="grid h-7 w-7 place-items-center rounded-[7px] bg-fg font-mono text-[13px] font-extrabold text-bg">X</span>
        <b className="hidden text-[13px] font-semibold sm:inline">XRAG</b>
      </a>
      <span className="text-line-strong">/</span>
      <KbSwitcher />
      <button
        type="button"
        onClick={onPalette}
        className="mx-auto hidden h-8 w-[min(460px,36vw)] items-center gap-2.5 rounded-lg border border-line bg-panel px-2.5 text-left text-[13px] text-faint hover:border-line-strong md:flex"
      >
        <Command className="h-3.5 w-3.5" />
        输入动作、页面或知识库…
        <Kbd className="ml-auto">⌘K</Kbd>
      </button>
      <div className="ml-auto flex items-center gap-3 text-xs text-dim md:ml-0">
        <Button variant="ghost" size="icon" className="md:hidden" onClick={onPalette} aria-label="打开命令面板">
          <Command />
        </Button>
        <span className="hidden items-center gap-1.5 lg:flex" title={system.workers ? '本进程内置执行器在处理任务' : '任务会排队，需要单独运行 xrag-server worker'}>
          <Dot tone={system.workers ? 'ok' : 'off'} />
          {system.workers ? '后台任务运行中' : '后台任务未启用'}
        </span>
        <span className="hidden font-mono lg:inline">{system.version}</span>
        <ThemeSwitcher />
        <Menu>
          <MenuTrigger asChild>
            <button type="button" className="flex h-7 items-center gap-1.5 rounded-md px-1.5 hover:bg-raised" aria-label="账号">
              <code className="max-w-[140px] truncate text-fg">{system.identity.name}</code>
              <ChevronDown className="h-3.5 w-3.5 text-faint" />
            </button>
          </MenuTrigger>
          <MenuContent>
            <MenuLabel>
              <span className="block font-mono text-fg">{system.identity.name}</span>
              {roleLabels[system.identity.role]}
              <span className="block">CLI profile：{profile}</span>
            </MenuLabel>
            <MenuSeparator />
            <MenuItem onSelect={() => void run(refresh, '已刷新服务状态')}>
              <RefreshCw />
              刷新服务状态
            </MenuItem>
            <MenuItem onSelect={() => navigate('/access')}>
              <Command />
              接入与 CLI profile
            </MenuItem>
            <MenuSeparator />
            <MenuItem onSelect={() => void logout()}>
              <LogOut />
              退出登录
            </MenuItem>
          </MenuContent>
        </Menu>
      </div>
    </header>
  )
}

type SidebarState = 'expanded' | 'collapsed'

function savedSidebar(): SidebarState | null {
  try {
    const saved = localStorage.getItem('xrag.sidebar')
    return saved === 'expanded' || saved === 'collapsed' ? saved : null
  } catch {
    return null
  }
}

/** 手动选过就用记住的值；没选过时按视口宽度，跨过 1280px 自动切换。 */
function useSidebar() {
  const [manual, setManual] = useState(savedSidebar)
  const [wide, setWide] = useState(() => matchMedia('(min-width: 1280px)').matches)
  const expanded = manual ? manual === 'expanded' : wide

  useEffect(() => {
    const query = matchMedia('(min-width: 1280px)')
    const update = () => setWide(query.matches)
    query.addEventListener('change', update)
    return () => query.removeEventListener('change', update)
  }, [])

  useEffect(() => {
    document.documentElement.dataset.sidebar = expanded ? 'expanded' : 'collapsed'
  }, [expanded])

  const toggle = useCallback(() => {
    const next: SidebarState = expanded ? 'collapsed' : 'expanded'
    setManual(next)
    try {
      localStorage.setItem('xrag.sidebar', next)
    } catch {
      // 隐私模式下不持久化。
    }
  }, [expanded])

  return { expanded, toggle }
}

export function Shell({ current, children }: { current: PageId; children: ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false)
  const [paletteOpen, setPaletteOpen] = useState(false)
  const { expanded, toggle } = useSidebar()

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      const key = event.key.toLowerCase()
      if ((event.metaKey || event.ctrlKey) && key === 'k') {
        event.preventDefault()
        setPaletteOpen(open => !open)
      } else if ((event.metaKey || event.ctrlKey) && key === 'b' && !event.shiftKey && !event.altKey) {
        event.preventDefault()
        toggle()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [toggle])

  return (
    <CurrentPageContext.Provider value={current}>
      <div className="flex h-dvh flex-col">
        <TopBar onMenu={() => setMenuOpen(true)} onPalette={() => setPaletteOpen(true)} />
        <div className="flex min-h-0 flex-1">
          <Rail current={current} expanded={expanded} onToggle={toggle} />
          <main className="min-w-0 flex-1 overflow-y-auto">{children}</main>
        </div>
        <MobileNav current={current} open={menuOpen} onOpenChange={setMenuOpen} />
        <Palette open={paletteOpen} onOpenChange={setPaletteOpen} sidebarExpanded={expanded} onToggleSidebar={toggle} />
      </div>
    </CurrentPageContext.Provider>
  )
}
