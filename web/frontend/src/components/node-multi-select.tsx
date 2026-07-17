import { useMemo } from 'react'
import { ArrowDown, ArrowUp, Check, GripVertical, X } from 'lucide-react'
import type { Node, NodeRef } from '@/lib/types'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'

interface NodeMultiSelectProps {
  nodes: Node[]
  value: NodeRef[]
  onChange: (next: NodeRef[]) => void
  single?: boolean
  reorderable?: boolean
}

// The selector is rendered inline (not in a Popover) so its scrollable list lives
// inside the Dialog's scroll region — otherwise the Dialog's scroll lock blocks
// touch scrolling on a portaled popover list.
export function NodeMultiSelect({
  nodes,
  value,
  onChange,
  single = false,
  reorderable = false,
}: NodeMultiSelectProps) {
  const selectedIds = useMemo(() => new Set(value.map((v) => v.id)), [value])

  const toggle = (node: Node) => {
    if (single) {
      onChange([{ id: node.id, name: node.name }])
      return
    }
    if (selectedIds.has(node.id)) {
      onChange(value.filter((v) => v.id !== node.id))
    } else {
      onChange([...value, { id: node.id, name: node.name }])
    }
  }

  const remove = (id: string) => onChange(value.filter((v) => v.id !== id))

  const move = (index: number, dir: -1 | 1) => {
    const target = index + dir
    if (target < 0 || target >= value.length) return
    const next = [...value]
    ;[next[index], next[target]] = [next[target], next[index]]
    onChange(next)
  }

  return (
    <div className="space-y-2">
      <Command className="rounded-md border border-border">
        <CommandInput placeholder="搜索节点名称…" />
        <CommandList className="max-h-56">
          <CommandEmpty>未找到节点</CommandEmpty>
          <CommandGroup>
            {nodes.map((node) => {
              const checked = selectedIds.has(node.id)
              return (
                <CommandItem
                  key={node.id}
                  value={`${node.name} ${node.server} ${node.region}`}
                  onSelect={() => toggle(node)}
                >
                  <div
                    className={cn(
                      'flex h-4 w-4 items-center justify-center rounded border',
                      checked
                        ? 'border-primary bg-primary text-primary-foreground'
                        : single
                          ? 'rounded-full border-input'
                          : 'border-input',
                    )}
                  >
                    {checked && <Check className="h-3 w-3" />}
                  </div>
                  <span className="flex-1 truncate">{node.name}</span>
                  {node.region && (
                    <Badge variant="muted" className="ml-auto shrink-0">
                      {node.region}
                    </Badge>
                  )}
                </CommandItem>
              )
            })}
          </CommandGroup>
        </CommandList>
      </Command>

      {value.length > 0 && (
        <div className="space-y-1.5">
          {reorderable ? (
            <ul className="divide-y divide-border rounded-md border border-border">
              {value.map((ref, i) => (
                <li key={ref.id} className="flex items-center gap-2 px-2 py-1.5 text-sm">
                  <GripVertical className="h-4 w-4 shrink-0 text-muted-foreground" />
                  <span className="w-5 shrink-0 text-xs text-muted-foreground">{i + 1}</span>
                  <span className="flex-1 truncate">{ref.name}</span>
                  <div className="flex shrink-0 items-center gap-0.5">
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      disabled={i === 0}
                      onClick={() => move(i, -1)}
                      aria-label="上移"
                    >
                      <ArrowUp className="h-4 w-4" />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      disabled={i === value.length - 1}
                      onClick={() => move(i, 1)}
                      aria-label="下移"
                    >
                      <ArrowDown className="h-4 w-4" />
                    </Button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      className="text-destructive"
                      onClick={() => remove(ref.id)}
                      aria-label="移除"
                    >
                      <X className="h-4 w-4" />
                    </Button>
                  </div>
                </li>
              ))}
            </ul>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {value.map((ref) => (
                <Badge key={ref.id} variant="secondary" className="gap-1 py-1 pr-1">
                  <span className="max-w-[12rem] truncate">{ref.name}</span>
                  <button
                    type="button"
                    onClick={() => remove(ref.id)}
                    className="rounded-sm hover:bg-background/40"
                    aria-label="移除"
                  >
                    <X className="h-3 w-3" />
                  </button>
                </Badge>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
