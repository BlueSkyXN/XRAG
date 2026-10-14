import { createContext, ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { ActionSpec, api, KbCaps, KnowledgeBase, Role, SystemInfo } from './api'

const rank: Record<Role, number> = { reader: 1, contributor: 2, maintainer: 3, admin: 4 }

export const roleLabels: Record<Role, string> = {
  reader: 'Reader · 查询',
  contributor: 'Contributor · 查询和提交',
  maintainer: 'Maintainer · 资料维护',
  admin: 'Admin · 全局管理'
}

/** 草稿依据：检索结果里的引用，攒起来交给“知识与 Wiki”写草稿。 */
export type Cite = { ref: string; label: string; kb: string }

export type Session = {
  system: SystemInfo
  kbs: KnowledgeBase[]
  kb: string
  kbName: string
  caps: KbCaps
  actions: ActionSpec[]
  setKb: (id: string) => void
  may: (role: Role) => boolean
  refresh: () => Promise<void>
  logout: () => Promise<void>
  /** 范围句柄（within）。只对当前知识库有效，切换知识库时清空。 */
  scope: { handle: string; label: string } | null
  setScope: (scope: { handle: string; label: string } | null) => void
  cites: Cite[]
  addCite: (cite: Cite) => void
  removeCite: (ref: string) => void
  clearCites: () => void
  /** CLI 示例里使用的 profile 名称，存在本机浏览器。 */
  profile: string
  setProfile: (name: string) => void
}

const SessionContext = createContext<Session | null>(null)

const stored = (key: string, fallback: string) => {
  try {
    return localStorage.getItem(key) ?? fallback
  } catch {
    return fallback
  }
}
const store = (key: string, value: string) => {
  try {
    localStorage.setItem(key, value)
  } catch {
    // 隐私模式下不持久化
  }
}

export function SessionProvider({
  initial,
  onLogout,
  children
}: {
  initial: { system: SystemInfo; kbs: KnowledgeBase[]; actions: ActionSpec[] }
  onLogout: () => void
  children: ReactNode
}) {
  const [system, setSystem] = useState(initial.system)
  const [kbs, setKbs] = useState(initial.kbs)
  const [kb, setKbState] = useState(() => {
    const saved = stored('xrag.kb', '')
    return initial.kbs.some(k => k.id === saved) ? saved : initial.kbs[0]?.id ?? ''
  })
  const [scope, setScope] = useState<Session['scope']>(null)
  const [cites, setCites] = useState<Cite[]>(() => {
    try {
      return JSON.parse(sessionStorage.getItem('xrag.cites') ?? '[]')
    } catch {
      return []
    }
  })
  const [profile, setProfileState] = useState(() => stored('xrag.profile', 'team'))

  useEffect(() => {
    try {
      sessionStorage.setItem('xrag.cites', JSON.stringify(cites))
    } catch {
      // 忽略存储失败
    }
  }, [cites])

  const setKb = useCallback((id: string) => {
    setKbState(id)
    setScope(null)
    store('xrag.kb', id)
  }, [])

  const refresh = useCallback(async () => {
    const [nextSystem, nextKbs] = await Promise.all([api.system(), api.kbs()])
    setSystem(nextSystem)
    setKbs(nextKbs.items)
    setKbState(current => (nextKbs.items.some(k => k.id === current) ? current : nextKbs.items[0]?.id ?? ''))
  }, [])

  const logout = useCallback(async () => {
    await api.logout().catch(() => undefined)
    try {
      sessionStorage.removeItem('xrag.cites')
    } catch {
      // 忽略
    }
    onLogout()
  }, [onLogout])

  const value = useMemo<Session>(() => {
    const role = system.identity.role
    return {
      system,
      kbs,
      kb,
      kbName: kbs.find(k => k.id === kb)?.name ?? kb,
      caps: { ...system.by_kb?.[kb] },
      actions: initial.actions,
      setKb,
      may: (need: Role) => (rank[role] ?? 0) >= rank[need],
      refresh,
      logout,
      scope,
      setScope,
      cites,
      addCite: cite => setCites(list => (list.some(c => c.ref === cite.ref) ? list : [...list, cite])),
      removeCite: ref => setCites(list => list.filter(c => c.ref !== ref)),
      clearCites: () => setCites([]),
      profile,
      setProfile: name => {
        setProfileState(name)
        store('xrag.profile', name)
      }
    }
  }, [system, kbs, kb, initial.actions, setKb, refresh, logout, scope, cites, profile])

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}

export function useSession() {
  const value = useContext(SessionContext)
  if (!value) throw new Error('SessionProvider missing')
  return value
}
