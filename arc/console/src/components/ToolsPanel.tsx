import { useState } from 'react'
import { Loader2 } from 'lucide-react'
import { initialValues, type FieldValues, type Tool, type ToolField } from '@/lib/catalog'
import { errorText } from '@/lib/pay'
import { AddressLink, ErrorNote, Mono, Panel } from './ui'

const input =
  'w-full rounded-md border border-white/10 bg-ink-950 px-2.5 py-1.5 font-mono text-[12px] text-slate-100 placeholder:text-slate-600 focus:border-accent/60 focus:outline-none'

function FieldInput(props: { field: ToolField; value: string; onChange: (v: string) => void; id: string }) {
  const { field, value, onChange, id } = props
  const hint = [field.description, field.min !== undefined || field.max !== undefined ? `${field.min ?? ''}..${field.max ?? ''}` : undefined]
    .filter(Boolean)
    .join(' / ')
  return (
    <div>
      <label htmlFor={id} className="mb-1 flex items-baseline gap-2 text-[11px] text-slate-400">
        <span className="font-mono text-slate-200">{field.name}</span>
        {field.required && <span className="text-[10px] uppercase tracking-wider text-accent">required</span>}
        {field.kind === 'stringArray' && <span className="text-slate-500">one per line</span>}
      </label>
      {field.kind === 'enum' || field.kind === 'boolean' ? (
        <select id={id} className={input} value={value} onChange={(e) => onChange(e.target.value)}>
          <option value="">(unset)</option>
          {field.enumValues?.map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </select>
      ) : field.kind === 'stringArray' ? (
        <textarea id={id} rows={3} className={input} value={value} onChange={(e) => onChange(e.target.value)} />
      ) : (
        <input
          id={id}
          className={input}
          inputMode={field.kind === 'integer' ? 'numeric' : undefined}
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
      {hint && <div className="mt-1 text-[11px] text-slate-500">{hint}</div>}
    </div>
  )
}

function ToolCard(props: {
  tool: Tool
  running: boolean
  active: boolean
  formError?: string
  onRun: (tool: Tool, values: FieldValues) => void
}) {
  const { tool } = props
  const [values, setValues] = useState<FieldValues>(() => initialValues(tool))
  const required = tool.fields.filter((f) => f.required)
  const optional = tool.fields.filter((f) => !f.required)
  const set = (name: string) => (v: string) => setValues((prev) => ({ ...prev, [name]: v }))

  return (
    <article className="rounded-lg border border-white/10 bg-ink-950/50 p-3.5">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <h3 className="font-display text-sm font-semibold text-slate-100">{tool.name}</h3>
          <p className="mt-0.5 text-xs text-slate-400">{tool.description}</p>
        </div>
        <div className="shrink-0 text-right">
          <div className="font-mono text-sm font-medium tabular-nums text-slate-100">{tool.priceUsdc} USDC</div>
          <div className="font-mono text-[10.5px] text-slate-500">{tool.accepts.amount} base units</div>
        </div>
      </div>
      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-slate-500">
        <span className="rounded border border-white/10 px-1.5 py-0.5 font-mono text-slate-300">{tool.method}</span>
        {tool.tags.map((t) => (
          <span key={t}>#{t}</span>
        ))}
        <span>
          payTo <AddressLink address={tool.accepts.payTo} />
        </span>
      </div>

      <div className="mt-3 space-y-2.5">
        {required.map((f) => (
          <FieldInput key={f.name} field={f} value={values[f.name] ?? ''} onChange={set(f.name)} id={`${tool.id}:${f.name}`} />
        ))}
        {optional.length > 0 && (
          <details className="rounded-md border border-white/10">
            <summary className="cursor-pointer px-2.5 py-1.5 text-[11px] text-slate-400 hover:text-slate-200">
              {optional.length} optional parameters
            </summary>
            <div className="space-y-2.5 border-t border-white/10 p-2.5">
              {optional.map((f) => (
                <FieldInput key={f.name} field={f} value={values[f.name] ?? ''} onChange={set(f.name)} id={`${tool.id}:${f.name}`} />
              ))}
            </div>
          </details>
        )}
      </div>

      {props.formError && (
        <div className="mt-3">
          <ErrorNote>{props.formError}</ErrorNote>
        </div>
      )}
      <button
        type="button"
        disabled={props.running}
        onClick={() => props.onRun(tool, values)}
        className="mt-3 inline-flex min-h-10 w-full items-center justify-center gap-2 rounded-lg bg-accent px-4 text-[13px] font-semibold text-ink-950 transition-colors hover:bg-white disabled:cursor-not-allowed disabled:opacity-50"
      >
        {props.running && props.active && <Loader2 size={14} className="animate-spin" />}
        {props.running && props.active ? 'Call in progress' : `Pay ${tool.priceUsdc} USDC & call`}
      </button>
    </article>
  )
}

export function ToolsPanel(props: {
  tools: Tool[] | undefined
  loading: boolean
  error: unknown
  running: boolean
  activeTool: string | undefined
  formError: { toolId: string; message: string } | undefined
  onRun: (tool: Tool, values: FieldValues) => void
}) {
  return (
    <Panel index="03" title="Paid tools" right={<Mono className="text-[11px] text-slate-500">GET /discovery/resources</Mono>}>
      {props.loading && (
        <div className="flex items-center gap-2 text-xs text-slate-400">
          <Loader2 size={14} className="animate-spin" /> Loading the live catalog
        </div>
      )}
      {props.error != null && <ErrorNote>Catalog request failed: {errorText(props.error)}</ErrorNote>}
      {props.tools && props.tools.length === 0 && (
        <p className="text-xs text-slate-400">The catalog returned no endpoints that accept exact USDC on eip155:5042002.</p>
      )}
      <div className="space-y-3">
        {props.tools?.map((tool) => (
          <ToolCard
            key={tool.id}
            tool={tool}
            running={props.running}
            active={props.activeTool === tool.name}
            formError={props.formError?.toolId === tool.id ? props.formError.message : undefined}
            onRun={props.onRun}
          />
        ))}
      </div>
    </Panel>
  )
}
