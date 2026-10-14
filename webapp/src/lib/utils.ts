import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/** 服务端时间戳为 Unix 秒；0 或空值显示占位。 */
export function when(seconds?: number, empty = '—') {
  if (!seconds) return empty
  return new Date(seconds * 1000).toLocaleString('zh-CN', { hour12: false })
}

export function shortTime(seconds?: number) {
  if (!seconds) return '—'
  const d = new Date(seconds * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function size(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1048576) return `${(bytes / 1024).toFixed(1)} KiB`
  return `${(bytes / 1048576).toFixed(1)} MiB`
}

/** 版本、任务等长编号只显示末尾，完整值放在 title 里。 */
export function tail(id = '', n = 8) {
  return id.length > n ? id.slice(-n) : id
}

export function lineRange(start?: number, end?: number) {
  if (!start) return ''
  return end && end !== start ? `${start}–${end}` : String(start)
}

export function pct(n: number) {
  return `${(n * 100).toFixed(1)}%`
}

export function lines(value: string) {
  return value
    .split('\n')
    .map(line => line.trim())
    .filter(Boolean)
}

export function commaList(value: string) {
  return value
    .split(',')
    .map(part => part.trim())
    .filter(Boolean)
}

export async function copyText(text: string) {
  await navigator.clipboard.writeText(text)
}

/** 命令行参数里的值需要引号时才加。 */
export function shellArg(value: string) {
  return /^[\w./:@%+=,-]+$/u.test(value) ? value : `'${value.replace(/'/g, `'\\''`)}'`
}

export type RefInfo = { kb: string; path: string; edition: string; chunk?: string; range?: [number, number]; result?: string }

/** 解开 xr1 引用（base64url 编码的 JSON），只用于展示；读取仍以服务端校验为准。 */
export function describeRef(ref: string): RefInfo | null {
  if (!ref.startsWith('xr1:')) return null
  try {
    const b64 = ref.slice(4).replace(/-/g, '+').replace(/_/g, '/')
    const bytes = Uint8Array.from(atob(b64 + '='.repeat((4 - (b64.length % 4)) % 4)), c => c.charCodeAt(0))
    const v = JSON.parse(new TextDecoder().decode(bytes))
    return { kb: v.k ?? '', path: v.p ?? '', edition: v.e ?? '', chunk: v.c, range: v.s !== undefined ? [v.s, v.t] : undefined, result: v.r }
  } catch {
    return null
  }
}
