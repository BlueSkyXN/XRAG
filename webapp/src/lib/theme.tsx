import { createContext, ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react'

export type ThemePreference = 'system' | 'dark' | 'light' | 'light-color'
export type ResolvedTheme = Exclude<ThemePreference, 'system'>

type ThemeValue = {
  preference: ThemePreference
  resolved: ResolvedTheme
  system: 'dark' | 'light'
  setPreference: (value: ThemePreference) => void
}

const preferences: ThemePreference[] = ['system', 'dark', 'light', 'light-color']
const ThemeContext = createContext<ThemeValue | null>(null)

function storedPreference(): ThemePreference {
  try {
    const value = localStorage.getItem('xrag.theme')
    return preferences.includes(value as ThemePreference) ? (value as ThemePreference) : 'system'
  } catch {
    return 'system'
  }
}

function resolvePreference(preference: ThemePreference, system: 'dark' | 'light'): ResolvedTheme {
  if (preference === 'system') return system
  return preference
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [system, setSystem] = useState<'dark' | 'light'>(() => (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark'))
  const [preference, setPreferenceState] = useState<ThemePreference>(storedPreference)
  const resolved = resolvePreference(preference, system)

  useEffect(() => {
    const query = matchMedia('(prefers-color-scheme: light)')
    const update = () => setSystem(query.matches ? 'light' : 'dark')
    query.addEventListener('change', update)
    return () => query.removeEventListener('change', update)
  }, [])

  useEffect(() => {
    const root = document.documentElement
    root.dataset.theme = resolved
    root.style.colorScheme = resolved === 'dark' ? 'dark' : 'light'
    const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]')
    if (meta) meta.content = getComputedStyle(root).getPropertyValue('--color-bg').trim()
  }, [resolved])

  const setPreference = useCallback((value: ThemePreference) => {
    setPreferenceState(value)
    try {
      localStorage.setItem('xrag.theme', value)
    } catch {
      // 隐私模式下不持久化。
    }
  }, [])

  const value = useMemo(() => ({ preference, resolved, system, setPreference }), [preference, resolved, system, setPreference])
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const value = useContext(ThemeContext)
  if (!value) throw new Error('ThemeProvider missing')
  return value
}
