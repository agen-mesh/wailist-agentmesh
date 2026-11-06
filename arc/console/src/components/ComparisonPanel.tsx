import { Panel } from './ui'

const rows: { label: string; arc: string; algorand: string }[] = [
  {
    label: 'Assets the payer must hold',
    arc: 'One: USDC pays for the call and for gas.',
    algorand: 'Two: the USDC ASA (after opting in) plus ALGO for fees.',
  },
  {
    label: 'Empty gas balance',
    arc: 'Cannot strand a funded agent: there is no separate gas balance. The facilitator relayer also submits the transfer.',
    algorand: 'Can strand a funded agent: USDC is useless without ALGO.',
  },
  {
    label: 'Gasless payment',
    arc: 'The payer signs one EIP-3009 authorization; the relayer sends it.',
    algorand: 'A 2-transaction fee-pooled group, cosigned by a shared facilitator.',
  },
]

export function ComparisonPanel() {
  return (
    <Panel index="06" title="Arc rail vs AgentMesh's Algorand rail">
      <div className="overflow-x-auto">
        <table className="w-full min-w-[560px] text-left text-xs leading-relaxed">
          <thead>
            <tr className="text-[10.5px] uppercase tracking-[0.1em] text-slate-500">
              <th className="pb-2 pr-4 font-medium" />
              <th className="pb-2 pr-4 font-medium text-accent">Arc Testnet</th>
              <th className="pb-2 font-medium">Algorand</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.label} className="border-t border-white/10 align-top">
                <td className="py-2 pr-4 text-slate-400">{r.label}</td>
                <td className="py-2 pr-4 text-slate-200">{r.arc}</td>
                <td className="py-2 text-slate-300">{r.algorand}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-3 text-xs text-slate-500">
        On Arc, USDC is the native gas token, so one funded balance covers both. Amounts here are always the 6-decimal ERC-20 view.
      </p>
    </Panel>
  )
}
