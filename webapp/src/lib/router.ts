import { useEffect, useState } from 'react'

// 路径路由：/search、/settings/keys。Server 对页面路径统一返回 index.html。
const EVENT = 'xrag:navigate'

export type Location = { path: string; segments: string[]; search: URLSearchParams }

function read(): Location {
  const path = window.location.pathname.replace(/\/+$/, '') || '/'
  return { path, segments: path.split('/').filter(Boolean), search: new URLSearchParams(window.location.search) }
}

export function navigate(to: string, replace = false) {
  if (to === window.location.pathname + window.location.search) return
  window.history[replace ? 'replaceState' : 'pushState'](null, '', to)
  window.dispatchEvent(new Event(EVENT))
  window.scrollTo(0, 0)
}

export function useLocation() {
  const [location, setLocation] = useState(read)
  useEffect(() => {
    const update = () => setLocation(read())
    window.addEventListener('popstate', update)
    window.addEventListener(EVENT, update)
    return () => {
      window.removeEventListener('popstate', update)
      window.removeEventListener(EVENT, update)
    }
  }, [])
  return location
}

/** 普通点击走客户端路由；带修饰键时交给浏览器新开标签。 */
export function linkHandler(to: string) {
  return (event: React.MouseEvent) => {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    event.preventDefault()
    navigate(to)
  }
}
