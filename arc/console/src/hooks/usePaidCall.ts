import { useCallback, useRef, useState } from 'react'
import { useAccount, useSignTypedData, useSwitchChain } from 'wagmi'
import type { Hex } from 'viem'
import { buildRequest, type FieldValues, type Tool } from '@/lib/catalog'
import { errorText, runPaidCall, type FlowState } from '@/lib/pay'
import { ARC_CHAIN_ID } from '@/lib/x402'

export interface Receipt {
  id: string
  toolName: string
  amountRaw: string
  payTo: string
  payer: string
  txHash?: string
  note?: string
  at: number
}

interface Options {
  onchainDomainSeparator: Hex | undefined
}

export function usePaidCall({ onchainDomainSeparator }: Options) {
  const { address, chainId, isConnected } = useAccount()
  const { signTypedDataAsync } = useSignTypedData()
  const { switchChainAsync } = useSwitchChain()
  const [flow, setFlow] = useState<FlowState | undefined>()
  const [running, setRunning] = useState(false)
  const [formError, setFormError] = useState<{ toolId: string; message: string } | undefined>()
  const [receipts, setReceipts] = useState<Receipt[]>([])
  const busy = useRef(false)

  const run = useCallback(
    async (tool: Tool, values: FieldValues) => {
      if (busy.current) return
      const request = buildRequest(tool, values)
      if (typeof request === 'string') {
        setFormError({ toolId: tool.id, message: request })
        return
      }
      setFormError(undefined)
      busy.current = true
      setRunning(true)
      try {
        const final = await runPaidCall({
          toolName: tool.name,
          request,
          payer: isConnected ? address : undefined,
          onchainDomainSeparator,
          sign: async (typed) => {
            if (chainId !== ARC_CHAIN_ID) {
              try {
                await switchChainAsync({ chainId: ARC_CHAIN_ID })
              } catch (e) {
                throw new Error(`Wallet is not on Arc Testnet (5042002) and switching failed: ${errorText(e)}`)
              }
            }
            return signTypedDataAsync({
              domain: typed.domain,
              types: typed.types,
              primaryType: typed.primaryType,
              message: typed.message,
            })
          },
          onUpdate: setFlow,
        })
        if (final.steps.settled.status === 'success' && final.accepted && address) {
          setReceipts((prev) => [
            {
              id: `${Date.now()}-${prev.length}`,
              toolName: tool.name,
              amountRaw: final.accepted?.amount ?? tool.accepts.amount,
              payTo: final.accepted?.payTo ?? tool.accepts.payTo,
              payer: address,
              txHash: final.settlement?.txHash,
              note: final.settlement?.txHash ? undefined : (final.settlementNote ?? 'No transaction hash returned'),
              at: Date.now(),
            },
            ...prev,
          ])
        }
      } finally {
        busy.current = false
        setRunning(false)
      }
    },
    [address, chainId, isConnected, onchainDomainSeparator, signTypedDataAsync, switchChainAsync],
  )

  return { flow, running, run, receipts, formError }
}
