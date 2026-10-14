import * as React from 'react'
import * as TabsPrimitive from '@radix-ui/react-tabs'
import { cn } from '../lib/utils'

const Tabs = TabsPrimitive.Root

const TabsList = React.forwardRef<React.ComponentRef<typeof TabsPrimitive.List>, React.ComponentPropsWithoutRef<typeof TabsPrimitive.List>>(
  ({ className, ...props }, ref) => <TabsPrimitive.List ref={ref} className={cn('flex shrink-0 items-end gap-1 overflow-x-auto overflow-y-hidden px-2 shadow-[inset_0_-1px_0_var(--color-line)]', className)} {...props} />
)
TabsList.displayName = 'TabsList'

const TabsTrigger = React.forwardRef<React.ComponentRef<typeof TabsPrimitive.Trigger>, React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger>>(
  ({ className, ...props }, ref) => (
    <TabsPrimitive.Trigger
      ref={ref}
      className={cn(
        'h-9 shrink-0 whitespace-nowrap border-b-2 border-transparent px-2.5 text-[12.5px] text-dim transition-colors hover:text-fg data-[state=active]:border-fg data-[state=active]:text-fg disabled:opacity-45',
        className
      )}
      {...props}
    />
  )
)
TabsTrigger.displayName = 'TabsTrigger'

const TabsContent = React.forwardRef<React.ComponentRef<typeof TabsPrimitive.Content>, React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>>(
  ({ className, ...props }, ref) => <TabsPrimitive.Content ref={ref} className={cn('min-h-0 outline-none', className)} {...props} />
)
TabsContent.displayName = 'TabsContent'

export { Tabs, TabsList, TabsTrigger, TabsContent }
