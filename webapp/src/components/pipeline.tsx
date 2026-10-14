import { ArrowRight } from 'lucide-react'
import { cn } from '../lib/utils'

export type Stage = {
  label: string
  value: string | number
  unit?: string
  /** 检索路线色；只有关键词、语义、图三条路线的阶段使用。 */
  route?: 'kw' | 'sem' | 'gr'
  /** 未执行（例如未配置的语义路线）。 */
  skipped?: boolean
  title?: string
}

const routeStyle = {
  kw: 'bg-kw/[0.07] shadow-[inset_0_2px_0_var(--color-kw)] [&_small]:text-kw',
  sem: 'bg-sem/[0.07] shadow-[inset_0_2px_0_var(--color-sem)] [&_small]:text-sem',
  gr: 'bg-gr/[0.06] shadow-[inset_0_2px_0_var(--color-gr)] [&_small]:text-gr'
}

/** 一次查询或一条资料链路的各阶段计数，按实际执行顺序排列。 */
export function Pipeline({ stages, total, className, label }: { stages: Stage[]; total?: { label: string; value: string }; className?: string; label: string }) {
  return (
    <section aria-label={label} className={cn('flex items-stretch overflow-x-auto rounded-[10px] border border-line bg-panel px-2 py-2', className)}>
      <ol className="flex items-stretch">
        {stages.map((stage, index) => (
          <li key={`${stage.label}-${index}`} className="flex items-stretch">
            {index > 0 && <ArrowRight aria-hidden="true" className="mx-0.5 h-3.5 w-3.5 self-center text-line-strong" />}
            <div
              title={stage.title}
              className={cn(
                'flex min-w-[84px] flex-col justify-center rounded-md px-2.5 py-1',
                stage.route && !stage.skipped && routeStyle[stage.route],
                stage.skipped && 'opacity-50 shadow-[inset_0_2px_0_var(--color-line-strong)]'
              )}
            >
              <small className="whitespace-nowrap text-[11.5px] text-faint">{stage.label}</small>
              <b className={cn('font-mono text-[15px] font-semibold tabular-nums', (stage.value === 0 || stage.skipped) && 'text-faint')}>
                {stage.value}
                {stage.unit && <span className="ml-1 font-sans text-[11px] font-normal text-faint">{stage.unit}</span>}
              </b>
            </div>
          </li>
        ))}
      </ol>
      {total && (
        <div className="ml-auto flex min-w-[150px] flex-col items-end justify-center border-l pl-4 pr-2">
          <small className="whitespace-nowrap text-[11.5px] text-faint">{total.label}</small>
          <b className="font-mono text-[15px] font-semibold">{total.value}</b>
        </div>
      )}
    </section>
  )
}
