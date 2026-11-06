import { getAddress, type Address, type Hex } from 'viem'
import { browserURL, type PaidRequest, type ToolAccept } from '@/lib/catalog'
import { ARC_CAIP2, ARC_USDC, assertSignableArcDomain, eip712DomainSeparator, isRecord } from '@/lib/x402'

export type StepStatus = 'idle' | 'pending' | 'success' | 'error'
export type StepKey = 'challenge' | 'sign' | 'retry' | 'settled'

export interface StepState {
  status: StepStatus
  message?: string
}

export interface TypedDomain {
  name: string
  version: string
  chainId: number
  verifyingContract: Address
}

export interface TransferMessage {
  from: Address
  to: Address
  value: bigint
  validAfter: bigint
  validBefore: bigint
  nonce: Hex
}

export interface TypedDataToSign {
  domain: TypedDomain
  types: {
    TransferWithAuthorization: readonly { name: string; type: string }[]
  }
  primaryType: 'TransferWithAuthorization'
  message: TransferMessage
}

export interface FlowState {
  toolName: string
  requestLine: string
  steps: Record<StepKey, StepState>
  challengeStatus?: number
  challengeBody?: string
  accepted?: { amount: string; payTo: string }
  challengeRaw?: string
  typed?: TypedDataToSign
  localDomainSeparator?: Hex
  domainMatchesToken?: boolean
  messageDisplay?: Record<string, string>
  signature?: Hex
  paymentHeaderBytes?: number
  retry?: { status: number; statusText: string; bodyText?: string }
  settlement?: { txHash?: string; payer?: string; decoded: unknown }
  settlementNote?: string
  dataText?: string
}

export const TRANSFER_TYPES = {
  TransferWithAuthorization: [
    { name: 'from', type: 'address' },
    { name: 'to', type: 'address' },
    { name: 'value', type: 'uint256' },
    { name: 'validAfter', type: 'uint256' },
    { name: 'validBefore', type: 'uint256' },
    { name: 'nonce', type: 'bytes32' },
  ],
} as const

const idle = (): StepState => ({ status: 'idle' })

function fresh(toolName: string, requestLine: string): FlowState {
  return {
    toolName,
    requestLine,
    steps: { challenge: idle(), sign: idle(), retry: idle(), settled: idle() },
  }
}

function step(flow: FlowState, key: StepKey, status: StepStatus, message?: string): FlowState {
  return { ...flow, steps: { ...flow.steps, [key]: { status, message } } }
}

export function errorText(error: unknown): string {
  if (error instanceof Error && error.message) return error.message
  return String(error)
}

export async function runPaidCall(options: {
  toolName: string
  request: PaidRequest
  payer: Address | undefined
  onchainDomainSeparator: Hex | undefined
  sign: (typed: TypedDataToSign) => Promise<Hex>
  onUpdate: (flow: FlowState) => void
}): Promise<FlowState> {
  const target = requestURL(options.request)
  let flow = fresh(options.toolName, `${options.request.method} ${options.request.url}`)
  const publish = (next: FlowState) => {
    flow = next
    options.onUpdate(next)
  }

  try {
    publish(step(flow, 'challenge', 'pending', 'requesting the unpaid call'))
    const challengeResponse = await callTool(options.request, target)
    const challengeBody = await challengeResponse.text()
    flow = {
      ...flow,
      challengeStatus: challengeResponse.status,
      challengeBody: challengeBody.slice(0, 500),
    }
    if (challengeResponse.status !== 402) {
      publish(step(flow, 'challenge', 'error', `expected HTTP 402, got ${challengeResponse.status}`))
      return flow
    }
    const challenge = decodeChallenge(challengeResponse, challengeBody)
    const accepted = pickAccept(challenge)
    if (!accepted) {
      publish(step(flow, 'challenge', 'error', `402 did not offer exact USDC on ${ARC_CAIP2}`))
      return flow
    }
    flow = {
      ...step(flow, 'challenge', 'success'),
      accepted: { amount: accepted.amount, payTo: accepted.payTo },
      challengeRaw: JSON.stringify(challenge, null, 2),
    }
    publish(flow)

    if (!options.payer) {
      publish(step(flow, 'sign', 'error', 'connect a wallet on Arc Testnet before paying'))
      return flow
    }
    publish(step(flow, 'sign', 'pending', 'building the EIP-3009 authorization'))
    const typed = buildTypedData(accepted, options.payer)
    const localDomainSeparator = eip712DomainSeparator(typed.domain)
    const domainMatchesToken = options.onchainDomainSeparator
      ? localDomainSeparator.toLowerCase() === options.onchainDomainSeparator.toLowerCase()
      : undefined
    flow = {
      ...flow,
      typed,
      localDomainSeparator,
      domainMatchesToken,
      messageDisplay: {
        from: typed.message.from,
        to: typed.message.to,
        value: typed.message.value.toString(),
        validAfter: typed.message.validAfter.toString(),
        validBefore: typed.message.validBefore.toString(),
        nonce: typed.message.nonce,
      },
    }
    publish(flow)
    assertSignableArcDomain(typed.domain, options.onchainDomainSeparator, localDomainSeparator)
    const signature = await options.sign(typed)
    flow = { ...step(flow, 'sign', 'success'), signature }
    publish(flow)

    publish(step(flow, 'retry', 'pending', 'retrying with the payment header'))
    const headerJSON = paymentJSON(challenge, accepted, typed, signature)
    const headerB64 = bytesToBase64(new TextEncoder().encode(headerJSON))
    flow = { ...flow, paymentHeaderBytes: headerB64.length }
    const paid = await callTool(options.request, target, {
      'Payment-Signature': headerB64,
      'X-Payment': headerJSON,
    })
    const paidBody = await paid.text()
    flow = {
      ...step(flow, 'retry', paid.ok ? 'success' : 'error', paid.ok ? undefined : `HTTP ${paid.status}`),
      retry: { status: paid.status, statusText: paid.statusText, bodyText: paid.ok ? undefined : paidBody.slice(0, 500) },
      dataText: paidBody.slice(0, 4000),
    }
    publish(flow)
    if (!paid.ok) return flow

    const settlement = decodeSettlement(paid, options.payer)
    publish({
      ...step(flow, 'settled', 'success'),
      settlement,
      settlementNote: settlement.txHash ? undefined : 'No transaction hash returned',
    })
    return flow
  } catch (error) {
    const message = errorText(error)
    const key: StepKey = flow.steps.sign.status === 'pending'
      ? 'sign'
      : flow.steps.retry.status === 'pending'
        ? 'retry'
        : flow.steps.settled.status === 'pending'
          ? 'settled'
          : 'challenge'
    publish(step(flow, key, 'error', message))
    return flow
  }
}

function requestURL(request: PaidRequest): string {
  const url = new URL(browserURL(request.url), window.location.origin)
  request.query.forEach((value, key) => url.searchParams.append(key, value))
  return url.toString()
}

async function callTool(request: PaidRequest, url: string, headers?: Record<string, string>): Promise<Response> {
  return fetch(url, {
    method: request.method,
    headers: {
      accept: 'application/json',
      ...(request.body ? { 'content-type': 'application/json' } : {}),
      ...headers,
    },
    body: request.body ? JSON.stringify(request.body) : undefined,
  })
}

function decodeChallenge(response: Response, body: string): Record<string, unknown> {
  const fromBody = parseRecord(body)
  if (fromBody && Array.isArray(fromBody.accepts) && fromBody.accepts.length > 0) return fromBody
  const header =
    decodeHeader(response.headers.get('payment-required')) ??
    decodeHeader(response.headers.get('x-payment-required'))
  if (header) return header
  if (fromBody) return fromBody
  throw new Error('402 response had no JSON challenge in the body or Payment-Required header')
}

function pickAccept(challenge: Record<string, unknown>): ToolAccept | undefined {
  if (!Array.isArray(challenge.accepts)) return undefined
  for (const entry of challenge.accepts) {
    if (!isRecord(entry) || entry.network !== ARC_CAIP2) continue
    const asset = typeof entry.asset === 'string' ? entry.asset : ''
    if (asset.toLowerCase() !== ARC_USDC.toLowerCase()) continue
    const amount = typeof entry.amount === 'string' ? entry.amount : typeof entry.maxAmountRequired === 'string' ? entry.maxAmountRequired : ''
    const payTo = typeof entry.payTo === 'string' ? entry.payTo : ''
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

function buildTypedData(accept: ToolAccept, payer: Address): TypedDataToSign {
  const name = typeof accept.extra.name === 'string' && accept.extra.name ? accept.extra.name : 'USDC'
  const version = typeof accept.extra.version === 'string' && accept.extra.version ? accept.extra.version : '2'
  const nonceBytes = new Uint8Array(32)
  crypto.getRandomValues(nonceBytes)
  const nonce = `0x${[...nonceBytes].map((b) => b.toString(16).padStart(2, '0')).join('')}` as Hex
  const validBefore = BigInt(Math.floor(Date.now() / 1000) + accept.maxTimeoutSeconds)
  return {
    domain: {
      name,
      version,
      chainId: 5042002,
      verifyingContract: getAddress(ARC_USDC),
    },
    types: TRANSFER_TYPES,
    primaryType: 'TransferWithAuthorization',
    message: {
      from: getAddress(payer),
      to: getAddress(accept.payTo),
      value: BigInt(accept.amount),
      validAfter: 0n,
      validBefore,
      nonce,
    },
  }
}

function paymentJSON(challenge: Record<string, unknown>, accept: ToolAccept, typed: TypedDataToSign, signature: Hex): string {
  return JSON.stringify({
    x402Version: 2,
    scheme: 'exact',
    network: accept.network,
    payload: {
      signature,
      authorization: {
        from: typed.message.from,
        to: typed.message.to,
        value: typed.message.value.toString(),
        validAfter: typed.message.validAfter.toString(),
        validBefore: typed.message.validBefore.toString(),
        nonce: typed.message.nonce,
      },
    },
    accepted: accept.raw,
    resource: challenge.resource,
    extensions: challenge.extensions,
    paymentRequired: challenge,
  })
}

function decodeSettlement(response: Response, payer: Address): { txHash?: string; payer?: string; decoded: unknown } {
  const raw = response.headers.get('payment-response') || response.headers.get('x-payment-response')
  const decoded = raw ? decodeHeader(raw) ?? parseRecord(raw) ?? raw : undefined
  const record = isRecord(decoded) ? decoded : undefined
  const txHash = record ? firstString(record, ['transaction', 'txHash', 'txId', 'txid', 'tx']) : undefined
  return { txHash, payer, decoded: decoded ?? {} }
}

function decodeHeader(raw: string | null): Record<string, unknown> | undefined {
  if (!raw) return undefined
  const direct = parseRecord(raw)
  if (direct) return direct
  try {
    const binary = atob(raw)
    const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0))
    return parseRecord(new TextDecoder().decode(bytes))
  } catch {
    return undefined
  }
}

function parseRecord(raw: string): Record<string, unknown> | undefined {
  try {
    const value: unknown = JSON.parse(raw)
    return isRecord(value) ? value : undefined
  } catch {
    return undefined
  }
}

function firstString(record: Record<string, unknown>, keys: string[]): string | undefined {
  for (const key of keys) {
    const value = record[key]
    if (typeof value === 'string' && value) return value
  }
  return undefined
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}
