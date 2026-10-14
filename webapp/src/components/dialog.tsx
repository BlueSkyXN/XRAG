import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { cn } from '../lib/utils'

const Dialog = DialogPrimitive.Root
const DialogTrigger = DialogPrimitive.Trigger
const DialogClose = DialogPrimitive.Close

type ContentProps = React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & {
  /** sheet：从右侧滑出的抽屉，用于原文、任务明细等长内容。 */
  side?: 'center' | 'sheet' | 'left'
}

const DialogContent = React.forwardRef<React.ComponentRef<typeof DialogPrimitive.Content>, ContentProps>(
  ({ className, children, side = 'center', ...props }, ref) => (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className="fixed inset-0 z-40 bg-black/60 animate-fade-in" />
      <DialogPrimitive.Content
        ref={ref}
        aria-describedby={undefined}
        className={cn(
          'fixed z-50 flex flex-col border border-line-strong bg-panel text-fg shadow-2xl shadow-black/60 outline-none',
          side === 'center' && 'left-1/2 top-[12vh] max-h-[80vh] w-[min(640px,calc(100vw-2rem))] -translate-x-1/2 rounded-xl animate-fade-in',
          side === 'sheet' && 'inset-y-0 right-0 w-[min(760px,100vw)] rounded-none border-y-0 border-r-0',
          side === 'left' && 'inset-y-0 left-0 w-[min(300px,86vw)] rounded-none border-y-0 border-l-0',
          className
        )}
        {...props}
      >
        {children}
        <DialogPrimitive.Close className="absolute right-3 top-3 rounded-md p-1 text-dim hover:bg-raised hover:text-fg">
          <X className="h-4 w-4" />
          <span className="sr-only">关闭</span>
        </DialogPrimitive.Close>
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  )
)
DialogContent.displayName = 'DialogContent'

const DialogTitle = React.forwardRef<
  React.ComponentRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn('shrink-0 border-b px-5 py-3.5 pr-12 text-[14px] font-semibold', className)} {...props} />
))
DialogTitle.displayName = 'DialogTitle'

function DialogBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('min-h-0 flex-1 overflow-y-auto px-5 py-4', className)} {...props} />
}

function DialogFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('flex shrink-0 items-center justify-end gap-2 border-t px-5 py-3', className)} {...props} />
}

export { Dialog, DialogTrigger, DialogClose, DialogContent, DialogTitle, DialogBody, DialogFooter }
