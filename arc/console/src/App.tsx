import { useRailInfo } from '@/hooks/useRail'
import { useCatalog } from '@/hooks/useCatalog'
import { usePaidCall } from '@/hooks/usePaidCall'
import { RailHeader } from '@/components/RailHeader'
import { WalletPanel } from '@/components/WalletPanel'
import { ToolsPanel } from '@/components/ToolsPanel'
import { FlowPanel } from '@/components/FlowPanel'
import { ReceiptsPanel } from '@/components/ReceiptsPanel'
import { ComparisonPanel } from '@/components/ComparisonPanel'

export default function App() {
  const rail = useRailInfo()
  const catalog = useCatalog()
  const { flow, running, run, receipts, formError } = usePaidCall({ onchainDomainSeparator: rail.token.domainSeparator })

  return (
    <div className="min-h-dvh bg-ink-950 pb-24">
      <div className="mx-auto max-w-7xl space-y-4 px-4 py-6">
        <header className="flex flex-wrap items-baseline justify-between gap-2">
          <h1 className="font-display text-xl font-bold tracking-tight text-slate-100">
            AgentMesh <span className="text-accent">Arc Rail</span>
          </h1>
          <p className="font-mono text-[11px] text-slate-500">x402 v2 exact / EIP-3009 / USDC / Arc Testnet</p>
        </header>

        <RailHeader rail={rail} />
        <WalletPanel usdc={rail.usdc} />

        <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
          <ToolsPanel
            tools={catalog.data}
            loading={catalog.isLoading}
            error={catalog.error}
            running={running}
            activeTool={flow?.toolName}
            formError={formError}
            onRun={(tool, values) => void run(tool, values)}
          />
          <FlowPanel flow={flow} />
        </div>

        <ReceiptsPanel receipts={receipts} />
        <ComparisonPanel />
      </div>
    </div>
  )
}
