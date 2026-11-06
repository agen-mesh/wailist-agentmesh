import { formatUsdc } from '@/onchain-money'
import type { Receipt } from '@/hooks/usePaidCall'
import { AddressLink, Mono, Panel, TxLink } from './ui'

export function ReceiptsPanel({ receipts }: { receipts: Receipt[] }) {
  return (
    <Panel index="05" title="Receipts" right={<span className="text-[11px] text-slate-500">this session, in memory only</span>}>
      {receipts.length === 0 ? (
        <p className="text-sm text-slate-400">No settled calls yet.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[640px] text-left text-xs">
            <thead>
              <tr className="text-[10.5px] uppercase tracking-[0.1em] text-slate-500">
                <th className="pb-2 pr-3 font-medium">Time</th>
                <th className="pb-2 pr-3 font-medium">Tool</th>
                <th className="pb-2 pr-3 text-right font-medium">Amount</th>
                <th className="pb-2 pr-3 font-medium">Payee</th>
                <th className="pb-2 font-medium">Transaction</th>
              </tr>
            </thead>
            <tbody>
              {receipts.map((r) => (
                <tr key={r.id} className="border-t border-white/10 align-top">
                  <td className="py-2 pr-3">
                    <Mono className="text-[11.5px]">{new Date(r.at).toLocaleTimeString()}</Mono>
                  </td>
                  <td className="py-2 pr-3 text-slate-200">{r.toolName}</td>
                  <td className="py-2 pr-3 text-right">
                    <Mono>{formatUsdc(BigInt(r.amountRaw))} USDC</Mono>
                  </td>
                  <td className="py-2 pr-3">
                    <AddressLink address={r.payTo} />
                  </td>
                  <td className="py-2">{r.txHash ? <TxLink hash={r.txHash} /> : <span className="text-amber-200">{r.note}</span>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </Panel>
  )
}
