import type { ReactNode } from 'react'
import { Check, X } from 'lucide-react'
import { formatUsdc } from '@/onchain-money'
import type { FlowState, StepKey } from '@/lib/pay'
import { ErrorNote, JsonBlock, Label, Mono, Panel, StatusIcon, TxLink } from './ui'

function Step(props: { n: number; title: string; state: FlowState['steps'][StepKey]; children?: ReactNode }) {
  const { state } = props
  return (
    <li className="relative rounded-lg border border-white/10 bg-ink-950/50 p-3.5">
      <div className="flex items-center gap-2.5">
        <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full border border-white/15 bg-ink-800">
          <StatusIcon status={state.status} />
        </span>
        <h3 className="font-display text-[13px] font-semibold text-slate-100">
          <span className="mr-2 font-mono text-[11px] text-slate-500">{props.n}</span>
          {props.title}
        </h3>
        <span className="ml-auto font-mono text-[10.5px] uppercase tracking-wider text-slate-500">{state.status}</span>
      </div>
      {state.message && state.status !== 'error' && <p className="mt-2 font-mono text-[12px] text-slate-400">{state.message}</p>}
      {state.status === 'error' && state.message && (
        <div className="mt-2">
          <ErrorNote>{state.message}</ErrorNote>
        </div>
      )}
      {props.children && <div className="mt-3 space-y-2.5">{props.children}</div>}
    </li>
  )
}

function KV({ k, children }: { k: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-[7.5rem_1fr] items-start gap-2">
      <Label>{k}</Label>
      <div className="min-w-0">{children}</div>
    </div>
  )
}

export function FlowPanel({ flow }: { flow: FlowState | undefined }) {
  if (!flow) {
    return (
      <Panel index="04" title="Call flow">
        <p className="text-sm text-slate-400">
          Run a paid tool to watch four steps with their real data: the 402 challenge, the EIP-3009 authorization, the retry with the payment header, and settlement.
        </p>
      </Panel>
    )
  }
  const { steps } = flow
  return (
    <Panel index="04" title="Call flow" right={<Mono className="text-[11px] text-slate-500">{flow.toolName}</Mono>}>
      <ol className="space-y-3">
        <Step n={1} title="GET -> 402 + decoded challenge" state={steps.challenge}>
          <KV k="Request">
            <Mono>{flow.requestLine}</Mono>
          </KV>
          {flow.challengeStatus !== undefined && (
            <KV k="Response">
              <Mono>HTTP {flow.challengeStatus}</Mono>
              {flow.challengeBody && <Mono className="ml-2 text-slate-400">{flow.challengeBody.slice(0, 160)}</Mono>}
            </KV>
          )}
          {flow.accepted && (
            <KV k="Price">
              <Mono>
                {formatUsdc(BigInt(flow.accepted.amount))} USDC ({flow.accepted.amount} base units, 6 decimals) to {flow.accepted.payTo}
              </Mono>
            </KV>
          )}
          {flow.challengeRaw && (
            <JsonBlock title="payment-required header, base64 decoded" value={flow.challengeRaw} defaultOpen />
          )}
        </Step>

        <Step n={2} title="Sign EIP-3009 TransferWithAuthorization" state={steps.sign}>
          {flow.typed && (
            <>
              <KV k="Domain">
                <Mono>
                  {flow.typed.domain.name} v{flow.typed.domain.version} / chainId {flow.typed.domain.chainId} / {flow.typed.domain.verifyingContract}
                </Mono>
              </KV>
              {flow.localDomainSeparator && (
                <KV k="Domain hash">
                  <div className="flex items-start gap-1.5">
                    <Mono className="text-[11.5px]">{flow.localDomainSeparator}</Mono>
                  </div>
                  {flow.domainMatchesToken !== undefined && (
                    <div className={`mt-1 inline-flex items-center gap-1 text-xs ${flow.domainMatchesToken ? 'text-emerald-300' : 'text-rose-300'}`}>
                      {flow.domainMatchesToken ? <Check size={12} /> : <X size={12} />}
                      {flow.domainMatchesToken
                        ? 'hash of the signing domain equals the token DOMAIN_SEPARATOR()'
                        : 'hash of the signing domain differs from the token DOMAIN_SEPARATOR()'}
                    </div>
                  )}
                </KV>
              )}
            </>
          )}
          {flow.messageDisplay && <JsonBlock title="Typed-data message" value={JSON.stringify(flow.messageDisplay, null, 2)} defaultOpen />}
          {flow.signature && (
            <KV k="Signature">
              <Mono className="text-[11.5px]">{flow.signature}</Mono>
            </KV>
          )}
        </Step>

        <Step n={3} title="Retry with the payment header" state={steps.retry}>
          {flow.paymentHeaderBytes !== undefined && (
            <KV k="Headers">
              <Mono>
                X-PAYMENT and PAYMENT-SIGNATURE: base64 JSON, {flow.paymentHeaderBytes} chars each
              </Mono>
            </KV>
          )}
          {flow.retry && (
            <KV k="Outcome">
              <Mono>
                HTTP {flow.retry.status} {flow.retry.statusText}
              </Mono>
              {flow.retry.status !== 200 && flow.retry.bodyText && <Mono className="ml-2 text-rose-300">{flow.retry.bodyText.slice(0, 400)}</Mono>}
            </KV>
          )}
        </Step>

        <Step n={4} title="Settled" state={steps.settled}>
          {flow.settlement?.txHash && (
            <KV k="Transaction">
              <TxLink hash={flow.settlement.txHash} />
            </KV>
          )}
          {flow.settlement?.payer && (
            <KV k="Payer">
              <Mono>{flow.settlement.payer}</Mono>
            </KV>
          )}
          {flow.settlementNote && <p className="font-mono text-[12px] text-amber-200">{flow.settlementNote}</p>}
          {flow.settlement && <JsonBlock title="Settlement header, decoded" value={JSON.stringify(flow.settlement.decoded, null, 2)} />}
          {flow.dataText !== undefined && <JsonBlock title="Paid response" value={flow.dataText} />}
        </Step>
      </ol>
    </Panel>
  )
}
