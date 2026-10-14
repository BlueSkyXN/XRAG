import { Fragment, useMemo } from 'react'

export function queryTerms(query: string) {
  return query.split(/\s+/).filter(term => term.length >= 2)
}

const escape = (value: string) => value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** 字母数字开头或结尾的词按词边界匹配：查 E203 不会标出 E2030 的前缀。中文按子串匹配。 */
export function termPattern(terms: string[]) {
  if (!terms.length) return null
  const parts = terms
    .slice()
    .sort((a, b) => b.length - a.length)
    .map(term => `${/^[A-Za-z0-9_]/.test(term) ? '(?<![A-Za-z0-9_])' : ''}${escape(term)}${/[A-Za-z0-9_]$/.test(term) ? '(?![A-Za-z0-9_])' : ''}`)
  return new RegExp(`(${parts.join('|')})`, 'gi')
}

export function Marked({ text, terms }: { text: string; terms: string[] }) {
  const pattern = useMemo(() => termPattern(terms), [terms])
  if (!pattern || !text) return <>{text}</>
  return (
    <>
      {text.split(pattern).map((part, index) => (index % 2 === 1 ? <mark key={index}>{part}</mark> : <Fragment key={index}>{part}</Fragment>))}
    </>
  )
}
