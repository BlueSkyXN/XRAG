import { createContext } from 'react'
import type { LucideIcon } from 'lucide-react'
import { BookOpen, ChartColumn, Compass, FolderTree, KeyRound, LayoutGrid, ListChecks, MessageSquareText, Search, SlidersHorizontal, Waypoints } from 'lucide-react'
import type { Role } from '../lib/api'

export type PageId = 'search' | 'graph' | 'research' | 'sources' | 'jobs' | 'knowledge' | 'feedback' | 'eval' | 'overview' | 'access' | 'settings'

export type PageDef = {
  id: PageId
  label: string
  group: '查找' | '建设' | '沉淀' | '评估' | '系统'
  icon: LucideIcon
  /** 导航里显示该页所需的最低角色；页内具体操作另行判断，最终以服务端权限为准。 */
  min: Role
  hint: string
}

export const pages: PageDef[] = [
  { id: 'search', label: '检索', group: '查找', icon: Search, min: 'reader', hint: '组合、关键词、语义检索与原文核对' },
  { id: 'graph', label: '关系', group: '查找', icon: Waypoints, min: 'reader', hint: '实体邻居、路径与关系出处' },
  { id: 'research', label: '调查', group: '查找', icon: Compass, min: 'reader', hint: '委托调查与固定检索配方' },
  { id: 'sources', label: '资料与来源', group: '建设', icon: FolderTree, min: 'reader', hint: '来源、构建与已发布文件' },
  { id: 'jobs', label: '任务', group: '建设', icon: ListChecks, min: 'reader', hint: '构建、Wiki、评测等后台任务' },
  { id: 'knowledge', label: '知识与 Wiki', group: '沉淀', icon: BookOpen, min: 'contributor', hint: '草稿、发布、写回与 Wiki' },
  { id: 'feedback', label: '反馈', group: '沉淀', icon: MessageSquareText, min: 'contributor', hint: '结果反馈与处理' },
  { id: 'eval', label: '评测', group: '评估', icon: ChartColumn, min: 'maintainer', hint: '题集、策略对比与查询诊断' },
  { id: 'overview', label: '概览', group: '系统', icon: LayoutGrid, min: 'reader', hint: '资料、任务和存储概况' },
  { id: 'access', label: '接入', group: '系统', icon: KeyRound, min: 'reader', hint: 'API Key、CLI 与 MCP 接入' },
  { id: 'settings', label: '设置', group: '系统', icon: SlidersHorizontal, min: 'reader', hint: '能力、模型、知识库配置与数据维护' }
]

export const pageById = Object.fromEntries(pages.map(p => [p.id, p])) as Record<PageId, PageDef>

/** PageHeader 根据当前页面显示多彩标识；默认值只为独立预览/测试兜底。 */
export const CurrentPageContext = createContext<PageId>('search')

/** 动作注册表里的动作在网页上由哪个页面承担。 */
export function pageForAction(action: string): string {
  if (['+ask', 'search.keyword', 'search.semantic', 'search.external', 'files.grep'].includes(action)) return `/search?action=${encodeURIComponent(action)}`
  if (action.startsWith('graph.') || action === '+context') return `/graph?action=${encodeURIComponent(action)}`
  if (action === 'files.tree' || action === 'files.read') return '/sources'
  if (action === 'kb.discover') return '/settings'
  return '/search'
}
