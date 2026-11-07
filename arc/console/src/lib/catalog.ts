import { formatUsdc } from '@/onchain-money'
import { ARC_CAIP2, ARC_USDC, isRecord } from '@/lib/x402'

export type FieldKind = 'string' | 'integer' | 'boolean' | 'enum' | 'stringArray'

export interface ToolField {
  name: string
  kind: FieldKind
  required: boolean
  description?: string
  min?: number
  max?: number
  enumValues?: string[]
}

export type FieldValues = Record<string, string>

export interface PaidRequest {
  method: 'GET' | 'POST'
  /** Absolute URL advertised by the catalog. */
  url: string
  query: URLSearchParams
  body?: Record<string, unknown>
}

export interface ToolAccept {
  amount: string
  payTo: string
  asset: string
  network: string
  extra: Record<string, unknown>
  maxTimeoutSeconds: number
  raw: Record<string, unknown>
}

export interface Tool {
  id: string
  name: string
  description: string
  priceUsdc: string
  method: 'GET' | 'POST'
  tags: string[]
  url: string
  fields: ToolField[]
  accepts: ToolAccept
  resource?: unknown
  extensions?: unknown
}

const TOOL_HOST = 'tinyx402.007575.xyz'
const FACILITATOR_HOST = 'arc.007575.xyz'

/** Same-origin path through the Vite dev proxies. Other hosts are refused. */
export function browserURL(raw: string): string {
  const url = new URL(raw)
  if (url.hostname === TOOL_HOST) return `/tool${url.pathname}${url.search}`
  if (url.hostname === FACILITATOR_HOST) return `/fac${url.pathname}${url.search}`
  throw new Error(`refusing to call ${url.hostname}`)
}

export function initialValues(tool: Tool): FieldValues {
  const values: FieldValues = {}
  for (const field of tool.fields) values[field.name] = ''
  return values
}

export function buildRequest(tool: Tool, values: FieldValues): string | PaidRequest {
  const query = new URLSearchParams()
  const body: Record<string, unknown> = {}
  let bodyFields = 0
  for (const field of tool.fields) {
    const raw = (values[field.name] ?? '').trim()
    if (!raw) {
      if (field.required) return `${field.name} is required`
      continue
    }
    const parsed = parseField(field, raw)
    if ('error' in parsed) return parsed.error
    if (tool.method === 'GET') {
      if (Array.isArray(parsed.value)) {
        for (const item of parsed.value) query.append(field.name, item)
      } else {
        query.set(field.name, String(parsed.value))
      }
    } else {
      body[field.name] = parsed.value
      bodyFields += 1
    }
  }
  return {
    method: tool.method,
    url: tool.url,
    query,
    body: tool.method === 'POST' && bodyFields > 0 ? body : undefined,
  }
}

function parseField(
  field: ToolField,
  raw: string,
): { value: string | number | boolean | string[] } | { error: string } {
  if (field.kind === 'integer') {
    if (!/^-?\d+$/.test(raw)) return { error: `${field.name} must be a whole number` }
    const n = Number(raw)
    if (field.min !== undefined && n < field.min) return { error: `${field.name} must be >= ${field.min}` }
    if (field.max !== undefined && n > field.max) return { error: `${field.name} must be <= ${field.max}` }
    return { value: n }
  }
  if (field.kind === 'boolean') {
    if (raw !== 'true' && raw !== 'false') return { error: `${field.name} must be true or false` }
    return { value: raw === 'true' }
  }
  if (field.kind === 'enum') {
    if (field.enumValues && !field.enumValues.includes(raw)) {
      return { error: `${field.name} must be one of ${field.enumValues.join(', ')}` }
    }
    return { value: raw }
  }
  if (field.kind === 'stringArray') {
    const items = raw.split('\n').map((line) => line.trim()).filter(Boolean)
    if (field.required && items.length === 0) return { error: `${field.name} is required` }
    return { value: items }
  }
  return { value: raw }
}

export async function fetchCatalog(signal: AbortSignal): Promise<Tool[]> {
  const tools: Tool[] = []
  const seen = new Set<string>()
  for (let page = 0; page < 5; page++) {
    const response = await fetch(`/fac/discovery/resources?limit=100&offset=${page * 100}`, { signal })
    if (!response.ok) throw new Error(`catalog HTTP ${response.status}`)
    const payload: unknown = await response.json()
    const items = catalogItems(payload)
    for (const item of items) {
      const tool = toTool(item)
      if (!tool || seen.has(tool.id)) continue
      seen.add(tool.id)
      tools.push(tool)
    }
    if (items.length < 100) break
  }
  tools.sort((a, b) => a.name.localeCompare(b.name))
  return tools
}

function catalogItems(payload: unknown): Record<string, unknown>[] {
  if (Array.isArray(payload)) return payload.filter(isRecord)
  if (!isRecord(payload)) return []
  const items = payload.items ?? payload.resources
  return Array.isArray(items) ? items.filter(isRecord) : []
}

function toTool(item: Record<string, unknown>): Tool | undefined {
  const url = resourceURL(item)
  if (!url) return undefined
  const accepts = arcAccept(item.accepts)
  if (!accepts) return undefined
  let amount: bigint
  try {
    amount = BigInt(accepts.amount)
  } catch {
    return undefined
  }
  if (amount <= 0n) return undefined
  const method = (stringField(item, 'method') || discoveryMethod(item) || 'GET').toUpperCase() === 'POST' ? 'POST' : 'GET'
  const name = toolName(url, stringField(item, 'description'))
  return {
    id: stringField(item, 'id') || url,
    name,
    description: stringField(item, 'description') || name,
    priceUsdc: formatUsdc(amount),
    method,
    tags: [],
    url,
    fields: fieldsFrom(item, method),
    accepts,
    resource: item.resource && typeof item.resource === 'object' ? item.resource : undefined,
    extensions: item.extensions,
  }
}

function arcAccept(raw: unknown): ToolAccept | undefined {
  if (!Array.isArray(raw)) return undefined
  for (const entry of raw) {
    if (!isRecord(entry)) continue
    if (entry.network !== ARC_CAIP2) continue
    const asset = stringField(entry, 'asset')
    if (asset.toLowerCase() !== ARC_USDC.toLowerCase()) continue
    const amount = stringField(entry, 'amount') || stringField(entry, 'maxAmountRequired')
    const payTo = stringField(entry, 'payTo')
    if (!amount || !payTo) continue
    const timeout = entry.maxTimeoutSeconds
    return {
      amount,
      payTo,
      asset,
      network: ARC_CAIP2,
      extra: isRecord(entry.extra) ? entry.extra : {},
      maxTimeoutSeconds: typeof timeout === 'number' && timeout > 0 ? timeout : 300,
      raw: entry,
    }
  }
  return undefined
}

function fieldsFrom(item: Record<string, unknown>, method: 'GET' | 'POST'): ToolField[] {
  const info = discoveryInfo(item)
  const schema = inputSchema(item)
  const key = method === 'GET' ? 'queryParams' : 'body'
  const spec = isRecord(schema[key]) ? schema[key] : undefined
  const examples = isRecord(info[key]) ? info[key] : undefined
  const properties = spec && isRecord(spec.properties) ? spec.properties : spec
  const required = new Set(Array.isArray(spec?.required) ? spec.required.filter((name): name is string => typeof name === 'string') : [])
  const names = new Set<string>([
    ...Object.keys(properties ?? {}),
    ...Object.keys(examples ?? {}),
  ])
  return [...names].sort().map((name) => fieldFrom(name, properties?.[name], required.has(name)))
}

function fieldFrom(name: string, spec: unknown, required: boolean): ToolField {
  const record = isRecord(spec) ? spec : {}
  const type = stringField(record, 'type')
  const enumValues = Array.isArray(record.enum) ? record.enum.filter((item): item is string => typeof item === 'string') : undefined
  let kind: FieldKind = 'string'
  if (enumValues && enumValues.length > 0) kind = type === 'boolean' ? 'boolean' : 'enum'
  else if (type === 'integer' || type === 'number') kind = 'integer'
  else if (type === 'boolean') kind = 'boolean'
  else if (type === 'array') kind = 'stringArray'
  const min = typeof record.minimum === 'number' ? record.minimum : undefined
  const max = typeof record.maximum === 'number' ? record.maximum : undefined
  const description = stringField(record, 'description') || undefined
  return { name, kind, required, description, min, max, enumValues: kind === 'boolean' ? ['true', 'false'] : enumValues }
}

function discoveryInfo(item: Record<string, unknown>): Record<string, unknown> {
  const direct = isRecord(item.discoveryInfo) ? item.discoveryInfo : undefined
  const extensions = isRecord(item.extensions) ? item.extensions : undefined
  const bazaar = extensions && isRecord(extensions.bazaar) ? extensions.bazaar : undefined
  const fromExt = bazaar && isRecord(bazaar.info) ? bazaar.info : undefined
  const info = direct ?? fromExt
  if (info && isRecord(info.input)) return info.input
  return info ?? {}
}

function discoveryMethod(item: Record<string, unknown>): string {
  const info = discoveryInfo(item)
  return stringField(info, 'method')
}

function inputSchema(item: Record<string, unknown>): Record<string, unknown> {
  const extensions = isRecord(item.extensions) ? item.extensions : undefined
  const bazaar = extensions && isRecord(extensions.bazaar) ? extensions.bazaar : undefined
  const schema = bazaar && isRecord(bazaar.schema) ? bazaar.schema : undefined
  const properties = schema && isRecord(schema.properties) ? schema.properties : undefined
  const input = properties && isRecord(properties.input) ? properties.input : undefined
  const inputProps = input && isRecord(input.properties) ? input.properties : undefined
  return inputProps ?? {}
}

function toolName(url: string, description: string): string {
  try {
    const path = new URL(url).pathname.split('/').filter(Boolean)
    const last = path[path.length - 1]
    if (last) return last
  } catch {
    /* fall through */
  }
  return description.slice(0, 48) || 'tool'
}

function resourceURL(item: Record<string, unknown>): string {
  const direct = stringField(item, 'resourceUrl') || stringField(item, 'url')
  if (direct) return direct
  if (typeof item.resource === 'string') return item.resource
  if (isRecord(item.resource) && typeof item.resource.url === 'string') return item.resource.url
  return ''
}

function stringField(record: Record<string, unknown>, key: string): string {
  const value = record[key]
  return typeof value === 'string' ? value : ''
}
