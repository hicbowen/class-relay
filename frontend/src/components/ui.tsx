import { cva, type VariantProps } from 'class-variance-authority'
import { clsx, type ClassValue } from 'clsx'
import { Dialog as Radix } from 'radix-ui'
import { twMerge } from 'tailwind-merge'
import { X } from 'lucide-react'
import { useRef, type ComponentProps, type ReactNode } from 'react'

export function cn(...inputs: ClassValue[]) { return twMerge(clsx(inputs)) }

const buttonVariants = cva('button', {
  variants: {
    variant: { primary: 'button-primary', outline: 'button-outline', ghost: 'button-ghost', danger: 'button-danger' },
    size: { default: '', icon: 'button-icon', small: 'button-small' },
  },
  defaultVariants: { variant: 'primary', size: 'default' },
})

export function Button({ variant, size, className, type = 'button', ...props }: ComponentProps<'button'> & VariantProps<typeof buttonVariants>) {
  return <button type={type} className={cn(buttonVariants({ variant, size }), className)} {...props} />
}

export function Dialog({ title, description, onClose, children }: {
  title: string; description?: string; onClose: () => void; children: ReactNode
}) {
  const previousFocus = useRef(document.activeElement)
  return <Radix.Root open onOpenChange={open => { if (!open) onClose() }}>
    <Radix.Portal>
      <Radix.Overlay className="dialog-overlay" />
      <Radix.Content className="dialog-content" {...(!description ? { "aria-describedby": undefined } : {})}
        onCloseAutoFocus={event => {
          event.preventDefault()
          if (previousFocus.current instanceof HTMLElement) previousFocus.current.focus()
        }}>
        <Radix.Title className="dialog-title">{title}</Radix.Title>
        {description && <Radix.Description className="dialog-description">{description}</Radix.Description>}
        <Radix.Close asChild><Button variant="ghost" size="icon" className="dialog-close" aria-label="关闭"><X size={19} /></Button></Radix.Close>
        {children}
      </Radix.Content>
    </Radix.Portal>
  </Radix.Root>
}

export function ErrorNotice({ children }: { children: ReactNode }) {
  return children ? <p role="alert" className="error-notice">{children}</p> : null
}
