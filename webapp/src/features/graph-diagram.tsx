import { useMemo } from 'react'

export type GraphEdge = { from: string; to: string; relation: string }

const NODE_H = 30
const COL_GAP = 120
const ROW_GAP = 22

const textWidth = (s: string) => [...s].reduce((w, ch) => w + (/[\u0000-ÿ]/.test(ch) ? 7.5 : 13.5), 0)

/**
 * 关系示意图：从起点按无向距离分层，左到右排列。只画这次查询返回的边，
 * 不代表全图；边上的关系名来自原文的显式标注或已配置的抽取器。
 */
export function GraphDiagram({ edges, start }: { edges: GraphEdge[]; start?: string }) {
  const layout = useMemo(() => {
    const names = [...new Set(edges.flatMap(e => [e.from, e.to]))]
    if (!names.length) return null
    const root = start && names.includes(start) ? start : names[0]
    const adj = new Map<string, Set<string>>()
    for (const e of edges) {
      adj.set(e.from, (adj.get(e.from) ?? new Set()).add(e.to))
      adj.set(e.to, (adj.get(e.to) ?? new Set()).add(e.from))
    }
    const depth = new Map<string, number>([[root, 0]])
    const queue = [root]
    while (queue.length) {
      const n = queue.shift()!
      for (const m of adj.get(n) ?? []) {
        if (!depth.has(m)) {
          depth.set(m, depth.get(n)! + 1)
          queue.push(m)
        }
      }
    }
    // 与起点不连通的节点放在最后一列之后
    let maxDepth = Math.max(...depth.values())
    for (const n of names) if (!depth.has(n)) depth.set(n, ++maxDepth)
    const columns: string[][] = []
    for (const n of names) (columns[depth.get(n)!] ??= []).push(n)
    const colWidth = columns.map(col => Math.max(...col.map(n => textWidth(n) + 28)))
    const height = Math.max(...columns.map(col => col.length)) * (NODE_H + ROW_GAP) - ROW_GAP
    const pos = new Map<string, { x: number; y: number; w: number }>()
    let x = 0
    columns.forEach((col, i) => {
      const top = (height - (col.length * (NODE_H + ROW_GAP) - ROW_GAP)) / 2
      col.forEach((n, j) => pos.set(n, { x: x + (colWidth[i] - (textWidth(n) + 28)) / 2, y: top + j * (NODE_H + ROW_GAP), w: textWidth(n) + 28 }))
      x += colWidth[i] + COL_GAP
    })
    return { pos, width: x - COL_GAP, height, root }
  }, [edges, start])

  if (!layout) return null
  const pad = 16
  return (
    <div className="overflow-x-auto">
      <svg
        role="img"
        aria-label={`关系图：${edges.map(e => `${e.from} ${e.relation} ${e.to}`).join('；')}`}
        viewBox={`${-pad} ${-pad} ${layout.width + pad * 2} ${layout.height + pad * 2}`}
        width={layout.width + pad * 2}
        height={layout.height + pad * 2}
        className="mx-auto block max-w-none"
      >
        <defs>
          <marker id="arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M0,0 L10,5 L0,10 z" fill="var(--color-gr)" />
          </marker>
        </defs>
        {edges.map((e, i) => {
          const a = layout.pos.get(e.from)!
          const b = layout.pos.get(e.to)!
          const forward = a.x <= b.x
          const x1 = forward ? a.x + a.w : a.x
          const x2 = forward ? b.x : b.x + b.w
          const y1 = a.y + NODE_H / 2
          const y2 = b.y + NODE_H / 2
          const mx = (x1 + x2) / 2
          const my = (y1 + y2) / 2
          return (
            <g key={i}>
              <path d={`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`} fill="none" stroke="var(--color-gr)" strokeOpacity="0.7" strokeWidth="1.4" markerEnd="url(#arrow)" />
              <text x={mx} y={my - 6} textAnchor="middle" fontSize="12" fill="var(--color-gr)" style={{ paintOrder: 'stroke', stroke: 'var(--color-panel)', strokeWidth: 4 }}>
                {e.relation}
              </text>
            </g>
          )
        })}
        {[...layout.pos.entries()].map(([name, p]) => (
          <g key={name}>
            <rect
              x={p.x}
              y={p.y}
              width={p.w}
              height={NODE_H}
              rx="7"
              fill={name === layout.root ? 'var(--color-fg)' : 'var(--color-raised)'}
              stroke={name === layout.root ? 'none' : 'var(--color-line-strong)'}
            />
            <text x={p.x + p.w / 2} y={p.y + NODE_H / 2 + 4.5} textAnchor="middle" fontSize="13" fill={name === layout.root ? 'var(--color-bg)' : 'var(--color-fg)'} fontWeight={name === layout.root ? 600 : 400}>
              {name}
            </text>
          </g>
        ))}
      </svg>
    </div>
  )
}
