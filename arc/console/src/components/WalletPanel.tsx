import { useAccount, useConnect, useDisconnect, useReadContract, useSwitchChain } from 'wagmi'
import { erc20Abi, type Address } from 'viem'
import { Loader2, Wallet } from 'lucide-react'
import { formatUsdc } from '@/onchain-money'
import { ARC_CHAIN_ID } from '@/lib/x402'
import { errorText } from '@/lib/pay'
import { ErrorNote, Label, Mono, Panel } from './ui'

const btn =
  'inline-flex min-h-9 items-center justify-center gap-2 rounded-lg px-3.5 text-[13px] font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-50'

export function WalletPanel({ usdc }: { usdc: Address }) {
  const { address, chainId, isConnected, isConnecting } = useAccount()
  const { connect, connectors, error: connectError, isPending } = useConnect()
  const { disconnect } = useDisconnect()
  const { switchChain, isPending: switching, error: switchError } = useSwitchChain()

  const balance = useReadContract({
    address: usdc,
    abi: erc20Abi,
    functionName: 'balanceOf',
    args: address ? [address] : undefined,
    chainId: ARC_CHAIN_ID,
    query: { enabled: Boolean(address), refetchInterval: 10_000 },
  })

  const hasProvider = typeof window !== 'undefined' && 'ethereum' in window
  const wrongChain = isConnected && chainId !== ARC_CHAIN_ID
  const raw = balance.data

  return (
    <Panel
      index="02"
      title="Wallet"
      right={
        isConnected ? (
          <button type="button" onClick={() => disconnect()} className="text-xs text-slate-400 hover:text-slate-200">
            Disconnect
          </button>
        ) : undefined
      }
    >
      {!isConnected ? (
        <div className="flex flex-wrap items-center gap-4">
          <button
            type="button"
            disabled={isPending || isConnecting || !hasProvider}
            onClick={() => connect({ connector: connectors[0] })}
            className={`${btn} bg-accent text-ink-950 hover:bg-white`}
          >
            {isPending || isConnecting ? <Loader2 size={14} className="animate-spin" /> : <Wallet size={14} />}
            Connect injected wallet
          </button>
          <p className="max-w-xl text-xs text-slate-400">
            {hasProvider
              ? 'The wallet only signs the EIP-3009 authorization. No key ever enters this page.'
              : 'No injected wallet (window.ethereum) was detected in this browser. The 402 challenge still works; signing needs a wallet.'}
          </p>
          {connectError && <ErrorNote>{errorText(connectError)}</ErrorNote>}
        </div>
      ) : (
        <div className="grid gap-4 sm:grid-cols-3">
          <div className="min-w-0">
            <Label>Address</Label>
            <Mono>{address}</Mono>
          </div>
          <div>
            <Label>USDC balance (ERC-20 view, 6 decimals)</Label>
            {balance.isLoading ? (
              <Loader2 size={14} className="animate-spin text-slate-400" />
            ) : balance.error ? (
              <span className="font-mono text-xs text-rose-300">balanceOf failed: {errorText(balance.error)}</span>
            ) : (
              <span className="font-mono text-xl font-medium tabular-nums text-slate-100">
                {raw !== undefined ? formatUsdc(raw) : '-'} <span className="text-sm text-slate-400">USDC</span>
              </span>
            )}
          </div>
          <div>
            <Label>Network</Label>
            {wrongChain ? (
              <div className="space-y-1.5">
                <div className="font-mono text-xs text-amber-300">wallet is on chainId {chainId}</div>
                <button
                  type="button"
                  disabled={switching}
                  onClick={() => switchChain({ chainId: ARC_CHAIN_ID })}
                  className={`${btn} bg-accent text-ink-950 hover:bg-white`}
                >
                  {switching && <Loader2 size={14} className="animate-spin" />}
                  Switch / add Arc Testnet
                </button>
                {switchError && <ErrorNote>{errorText(switchError)}</ErrorNote>}
              </div>
            ) : (
              <span className="font-mono text-xs text-emerald-300">Arc Testnet ({ARC_CHAIN_ID})</span>
            )}
          </div>
          {raw === 0n && !wrongChain && (
            <p className="text-xs text-amber-200 sm:col-span-3">
              Balance is 0. You need Arc Testnet USDC and nothing else (it pays for the call and for gas):{' '}
              <a
                href="https://faucet.circle.com"
                target="_blank"
                rel="noopener noreferrer"
                className="text-accent underline underline-offset-2"
              >
                faucet.circle.com
              </a>
              . You can still run a call to see the facilitator's real rejection.
            </p>
          )}
        </div>
      )}
    </Panel>
  )
}
