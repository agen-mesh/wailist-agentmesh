import { Check, Loader2, X } from 'lucide-react'
import { TokenUSDC } from '@web3icons/react'
import type { RailInfo } from '@/hooks/useRail'
import { ARC_CAIP2, ARC_CHAIN_ID } from '@/lib/x402'
import { AddressLink, CopyButton, ErrorNote, Label, Mono, Panel } from './ui'

function Field(props: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <Label>{props.label}</Label>
      <div className="text-sm text-slate-200">{props.children}</div>
    </div>
  )
}

function SeparatorRow(props: { label: string; source: string; value?: string; loading: boolean; error?: string }) {
  return (
    <div className="min-w-0 rounded-lg border border-white/10 bg-ink-950/60 px-3 py-2">
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="text-[10.5px] font-medium uppercase tracking-[0.1em] text-slate-500">{props.label}</span>
        <span className="font-mono text-[10.5px] text-slate-500">{props.source}</span>
      </div>
      {props.value ? (
        <div className="flex items-start gap-1.5">
          <Mono className="text-[11.5px]">{props.value}</Mono>
          <CopyButton value={props.value} label={props.label} />
        </div>
      ) : props.loading ? (
        <span className="inline-flex items-center gap-1.5 text-xs text-slate-400">
          <Loader2 size={12} className="animate-spin" /> reading
        </span>
      ) : (
        <span className="font-mono text-xs text-rose-300">{props.error ?? 'unavailable'}</span>
      )}
    </div>
  )
}

export function RailHeader({ rail }: { rail: RailInfo }) {
  const { facilitator, token, separatorsMatch } = rail
  const verdict =
    separatorsMatch === true ? (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-emerald-400/30 bg-emerald-400/10 px-2.5 py-1 text-[11px] font-medium text-emerald-300">
        <Check size={12} /> signing domain verified
      </span>
    ) : separatorsMatch === false ? (
      <span className="inline-flex items-center gap-1.5 rounded-full border border-rose-400/30 bg-rose-400/10 px-2.5 py-1 text-[11px] font-medium text-rose-300">
        <X size={12} /> domain separators differ
      </span>
    ) : (
      <span className="inline-flex items-center gap-1.5 text-[11px] text-slate-400">
        <Loader2 size={12} className="animate-spin" /> checking
      </span>
    )

  return (
    <Panel index="01" title="Rail" right={verdict}>
      <div className="grid gap-x-6 gap-y-4 sm:grid-cols-2 lg:grid-cols-4">
        <Field label="Network">
          <div className="font-display font-semibold">{rail.chainName}</div>
          <Mono>
            chainId {ARC_CHAIN_ID} / {ARC_CAIP2}
          </Mono>
        </Field>
        <Field label="USDC (6-decimal ERC-20 interface)">
          <div className="flex items-start gap-1.5">
            <TokenUSDC variant="branded" size={16} className="mt-0.5 shrink-0" />
            <Mono>{rail.usdc}</Mono>
            <CopyButton value={rail.usdc} label="USDC address" />
          </div>
          <div className="mt-1 text-xs text-slate-400">
            name() {token.name ? `"${token.name}"` : '...'} / version() {token.version ? `"${token.version}"` : '...'} / decimals() {token.decimals ?? '...'}
          </div>
        </Field>
        <Field label="Facilitator">
          <Mono>arc.007575.xyz</Mono>
          <div className="mt-1 text-xs text-slate-400">
            {rail.facilitatorLoading
              ? 'reading /supported'
              : facilitator
                ? `/supported: exact on ${facilitator.networks.join(', ')}${facilitator.supportsArcExact ? '' : ' (no Arc Testnet!)'}`
                : 'unreachable'}
          </div>
        </Field>
        <Field label="Explorer">
          <a
            href="https://explorer.testnet.arc.io"
            target="_blank"
            rel="noopener noreferrer"
            className="font-mono text-[12px] text-accent underline-offset-2 hover:underline"
          >
            explorer.testnet.arc.io
          </a>
          <div className="mt-1 text-xs text-slate-400">
            token on explorer: <AddressLink address={rail.usdc} />
          </div>
        </Field>
      </div>

      <div className="mt-4 grid gap-3 md:grid-cols-2">
        <SeparatorRow
          label="Domain separator"
          source="facilitator GET /assets"
          value={facilitator?.asset.domainSeparator}
          loading={rail.facilitatorLoading}
          error={rail.facilitatorError}
        />
        <SeparatorRow
          label="Domain separator"
          source="token DOMAIN_SEPARATOR() eth_call 0x3644e515"
          value={token.domainSeparator}
          loading={rail.tokenLoading}
          error={rail.tokenError}
        />
      </div>
      <p className="mt-2 text-xs text-slate-500">
        If both values match, the EIP-712 domain this console signs under is the one the token enforces.
      </p>
      {rail.facilitatorError && <div className="mt-3"><ErrorNote>Facilitator: {rail.facilitatorError}</ErrorNote></div>}
    </Panel>
  )
}
