import * as React from 'react'
import { cn } from '../lib/utils'

export function Table({ className, ...props }: React.TableHTMLAttributes<HTMLTableElement>) {
  return (
    <div className="min-w-0 overflow-x-auto">
      <table className={cn('w-full border-collapse text-[12.5px]', className)} {...props} />
    </div>
  )
}

export function Th({ className, ...props }: React.ThHTMLAttributes<HTMLTableCellElement>) {
  return <th className={cn('whitespace-nowrap border-b px-4 py-2 text-left font-normal text-faint', className)} {...props} />
}

export function Td({ className, ...props }: React.TdHTMLAttributes<HTMLTableCellElement>) {
  return <td className={cn('border-b px-4 py-2.5 align-middle text-fg-soft [tr:last-child_&]:border-b-0', className)} {...props} />
}
