import * as React from 'react'
import * as MenuPrimitive from '@radix-ui/react-dropdown-menu'
import { cn } from '../lib/utils'

const Menu = MenuPrimitive.Root
const MenuTrigger = MenuPrimitive.Trigger

const MenuContent = React.forwardRef<
  React.ComponentRef<typeof MenuPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof MenuPrimitive.Content>
>(({ className, sideOffset = 6, align = 'end', ...props }, ref) => (
  <MenuPrimitive.Portal>
    <MenuPrimitive.Content
      ref={ref}
      sideOffset={sideOffset}
      align={align}
      className={cn('z-50 min-w-52 rounded-lg border border-line-strong bg-raised p-1 shadow-xl shadow-black/50 animate-fade-in', className)}
      {...props}
    />
  </MenuPrimitive.Portal>
))
MenuContent.displayName = 'MenuContent'

const MenuItem = React.forwardRef<
  React.ComponentRef<typeof MenuPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof MenuPrimitive.Item>
>(({ className, ...props }, ref) => (
  <MenuPrimitive.Item
    ref={ref}
    className={cn(
      'flex h-8 cursor-default select-none items-center gap-2 rounded-md px-2 text-[13px] text-fg outline-none data-[highlighted]:bg-hover data-[disabled]:opacity-45 [&_svg]:h-4 [&_svg]:w-4 [&_svg]:text-dim',
      className
    )}
    {...props}
  />
))
MenuItem.displayName = 'MenuItem'

const MenuLabel = React.forwardRef<
  React.ComponentRef<typeof MenuPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof MenuPrimitive.Label>
>(({ className, ...props }, ref) => (
  <MenuPrimitive.Label ref={ref} className={cn('px-2 py-1.5 text-xs text-dim', className)} {...props} />
))
MenuLabel.displayName = 'MenuLabel'

const MenuSeparator = React.forwardRef<
  React.ComponentRef<typeof MenuPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof MenuPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <MenuPrimitive.Separator ref={ref} className={cn('-mx-1 my-1 h-px bg-line', className)} {...props} />
))
MenuSeparator.displayName = 'MenuSeparator'

export { Menu, MenuTrigger, MenuContent, MenuItem, MenuLabel, MenuSeparator }
