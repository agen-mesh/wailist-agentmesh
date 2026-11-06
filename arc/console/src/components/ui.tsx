import { useState, type ReactNode } from 'react'
import { Check, ChevronRight, Copy, ExternalLink, Loader2, X } from 'lucide-react'
import clsx from 'clsx'
import { buildTxExplorerUrl, buildAddressExplorerUrl } from '@/onchain-facts'
import { ARC_CHAIN_ID } from '@/lib/x402'
import type { StepStatus } from '@/lib/pay'

export function Panel(props: { index: string; title: string; right?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={clsx('rounded-xl border border-white/10 bg-ink-900/80', props.className)}>
      <header className="flex items-center justify-between gap-3 border-b border-white/10 px-4 py-2.5">
        <h2 className="flex items-center gap-2.5 font-display text-[13px] font-semibold uppercase tracking-[0.08em] text-slate-200">
          <span className="font-mono text-[11px] text-accent/70">{props.index}</span>
          {props.title}
        </h2>
        {props.right}
      </header>
      <div className="p-4">{props.children}</div>
    </section>
  )
}

export function Label({ children }: { children: ReactNode }) {
  return <div className="mb-1 text-[10.5px] font-medium uppercase tracking-[0.1em] text-slate-500">{children}</div>
}

export function Mono({ children, className }: { children: ReactNode; className?: string }) {
  return <span className={clsx('break-all font-mono text-[12px] tabular-nums text-slate-200', className)}>{children}</span>
}

export function short(value: string, head = 6, tail = 4): string {
  return value.length > head + tail + 3 ? `${value.slice(0, head)}...${value.slice(-tail)}` : value
}

export function CopyButton({ value, label }: { value: string; label: string }) {
  const [done, setDone] = useState(false)
  return (
    <button
      type="button"
      aria-label={`Copy ${label}`}
      onClick={() => {
        void navigator.clipboard.writeText(value).then(() => {
          setDone(true)
          setTimeout(() => setDone(false), 1200)
        })
      }}
      className="inline-flex h-5 w-5 items-center justify-center rounded text-slate-500 hover:text-accent"
    >
      {done ? <Check size={12} /> : <Copy size={12} />}
    </button>
  )
}

export function TxLink({ hash }: { hash: string }) {
  return (
    <a
      href={buildTxExplorerUrl(ARC_CHAIN_ID, hash)}
      target="_blank"
      rel="noopener noreferrer"
      className="inline-flex items-center gap-1 font-mono text-[12px] text-accent underline-offset-2 hover:underline"
    >
      {short(hash, 10, 8)}
      <ExternalLink size={11} />
    </a>
  )
}

export function AddressLink({ address }: { address: string }) {
  return (
    <a
      href={buildAddressExplorerUrl(ARC_CHAIN_ID, address)}
      target="_blank"
      rel="noopener noreferrer"
      className="font-mono text-[12px] text-slate-200 underline-offset-2 hover:text-accent hover:underline"
    >
      {short(address, 8, 6)}
    </a>
  )
}

export function JsonBlock(props: { title: string; value: string; defaultOpen?: boolean }) {
  return (
    <details open={props.defaultOpen} className="group rounded-lg border border-white/10 bg-ink-950/70">
      <summary className="flex cursor-pointer items-center gap-1.5 px-3 py-2 text-[11px] font-medium uppercase tracking-[0.08em] text-slate-400 hover:text-slate-200">
        <ChevronRight size={12} className="transition-transform group-open:rotate-90" />
        {props.title}
        <span className="ml-auto">
          <CopyButton value={props.value} label={props.title} />
        </span>
      </summary>
      <pre className="max-h-72 overflow-auto border-t border-white/10 px-3 py-2.5 font-mono text-[11.5px] leading-relaxed text-slate-300">
        {props.value}
      </pre>
    </details>
  )
}

export function StatusIcon({ status }: { status: StepStatus }) {
  if (status === 'pending') return <Loader2 size={14} className="animate-spin text-accent" />
  if (status === 'success') return <Check size={14} className="text-emerald-400" />
  if (status === 'error') return <X size={14} className="text-rose-400" />
  return <span className="block h-2 w-2 rounded-full bg-slate-600" />
}

export function ErrorNote({ children }: { children: ReactNode }) {
  return (
    <div role="alert" className="rounded-lg border border-rose-500/30 bg-rose-500/10 px-3 py-2 font-mono text-[12px] leading-relaxed text-rose-200">
      {children}
    </div>
  )
}
