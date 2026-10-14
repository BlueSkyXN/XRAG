import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '../lib/utils'

const buttonVariants = cva(
  'inline-flex shrink-0 items-center justify-center gap-1.5 whitespace-nowrap rounded-md font-medium transition-colors disabled:pointer-events-none disabled:opacity-45 [&_svg]:h-[15px] [&_svg]:w-[15px]',
  {
    variants: {
      variant: {
        primary: 'bg-fg text-bg hover:bg-white',
        outline: 'border border-line-strong bg-transparent text-fg hover:bg-hover',
        subtle: 'bg-raised text-fg hover:bg-hover',
        ghost: 'text-dim hover:bg-raised hover:text-fg',
        danger: 'border border-err/40 text-err hover:bg-err/10'
      },
      size: {
        sm: 'h-7 px-2.5 text-xs',
        md: 'h-8 px-3 text-[13px]',
        lg: 'h-9 px-4 text-[13px]',
        icon: 'h-8 w-8',
        'icon-sm': 'h-7 w-7'
      }
    },
    defaultVariants: { variant: 'outline', size: 'md' }
  }
)

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement>, VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(({ className, variant, size, asChild = false, type, ...props }, ref) => {
  const Comp = asChild ? Slot : 'button'
  return <Comp ref={ref} type={asChild ? undefined : (type ?? 'button')} className={cn(buttonVariants({ variant, size }), className)} {...props} />
})
Button.displayName = 'Button'

export { Button, buttonVariants }
