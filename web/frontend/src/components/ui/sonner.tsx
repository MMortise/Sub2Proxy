import { Toaster as Sonner } from 'sonner'
import { useTheme } from '@/components/theme-provider'

export function Toaster() {
  const { resolvedTheme } = useTheme()
  return (
    <Sonner
      theme={resolvedTheme}
      position="top-right"
      richColors
      closeButton
      toastOptions={{
        classNames: {
          toast:
            'group border border-border bg-popover text-popover-foreground shadow-lg',
        },
      }}
    />
  )
}
