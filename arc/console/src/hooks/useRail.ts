import { useQuery } from '@tanstack/react-query'
import { useReadContracts } from 'wagmi'
import { parseAbi, type Address, type Hex } from 'viem'
import { getUsdc, requireChain } from '@/onchain-facts'
import { ARC_CAIP2, ARC_CHAIN_ID, isRecord } from '@/lib/x402'

const tokenAbi = parseAbi([
  'function name() view returns (string)',
  'function version() view returns (string)',
  'function decimals() view returns (uint8)',
  'function DOMAIN_SEPARATOR() view returns (bytes32)',
])

export interface FacilitatorAsset {
  address: string
  decimals: number
  domainSeparator: string
  name?: string
  version?: string
}

export interface FacilitatorInfo {
  asset: FacilitatorAsset
  supportsArcExact: boolean
  networks: string[]
}

async function fetchFacilitator(): Promise<FacilitatorInfo> {
  const [assetsRes, supportedRes] = await Promise.all([fetch('/fac/assets'), fetch('/fac/supported')])
  if (!assetsRes.ok) throw new Error(`/assets answered HTTP ${assetsRes.status}`)
  if (!supportedRes.ok) throw new Error(`/supported answered HTTP ${supportedRes.status}`)
  const assets: unknown = await assetsRes.json()
  const supported: unknown = await supportedRes.json()
  if (!isRecord(assets) || !Array.isArray(assets.networks)) throw new Error('/assets returned an unexpected shape')
  const net = assets.networks.find((n): n is Record<string, unknown> => isRecord(n) && n.caip2 === ARC_CAIP2)
  const first: unknown = net && Array.isArray(net.assets) ? (net.assets as unknown[])[0] : undefined
  if (!isRecord(first) || typeof first.address !== 'string' || typeof first.domainSeparator !== 'string') {
    throw new Error(`/assets has no asset for ${ARC_CAIP2}`)
  }
  const extra = isRecord(first.extra) ? first.extra : {}
  const kinds = isRecord(supported) && Array.isArray(supported.kinds) ? supported.kinds : []
  const networks = kinds
    .filter((k): k is Record<string, unknown> => isRecord(k) && k.scheme === 'exact')
    .map((k) => String(k.network))
  return {
    asset: {
      address: first.address,
      decimals: typeof first.decimals === 'number' ? first.decimals : 6,
      domainSeparator: first.domainSeparator,
      name: typeof extra.name === 'string' ? extra.name : undefined,
      version: typeof extra.version === 'string' ? extra.version : undefined,
    },
    supportsArcExact: networks.includes(ARC_CAIP2),
    networks,
  }
}

export interface RailInfo {
  chainName: string
  usdc: Address
  facilitator: FacilitatorInfo | undefined
  facilitatorError: string | undefined
  facilitatorLoading: boolean
  /** Values read from the token itself over RPC. */
  token: { name?: string; version?: string; decimals?: number; domainSeparator?: Hex }
  tokenError: string | undefined
  tokenLoading: boolean
  /** true/false once both separators are known, otherwise undefined. */
  separatorsMatch: boolean | undefined
}

export function useRailInfo(): RailInfo {
  const chain = requireChain(ARC_CHAIN_ID)
  const usdcFact = getUsdc(ARC_CHAIN_ID)
  if (!usdcFact) throw new Error('USDC is not registered for Arc Testnet')
  const usdc = usdcFact.address as Address

  const fac = useQuery({ queryKey: ['facilitator'], queryFn: fetchFacilitator, staleTime: 60_000, retry: 1 })
  const reads = useReadContracts({
    allowFailure: true,
    contracts: [
      { address: usdc, abi: tokenAbi, functionName: 'name', chainId: ARC_CHAIN_ID },
      { address: usdc, abi: tokenAbi, functionName: 'version', chainId: ARC_CHAIN_ID },
      { address: usdc, abi: tokenAbi, functionName: 'decimals', chainId: ARC_CHAIN_ID },
      { address: usdc, abi: tokenAbi, functionName: 'DOMAIN_SEPARATOR', chainId: ARC_CHAIN_ID },
    ],
  })

  const [n, v, d, s] = reads.data ?? []
  const domainSeparator = s?.status === 'success' ? s.result : undefined
  const facSep = fac.data?.asset.domainSeparator
  return {
    chainName: chain.name,
    usdc,
    facilitator: fac.data,
    facilitatorError: fac.error ? fac.error.message : undefined,
    facilitatorLoading: fac.isLoading,
    token: {
      name: n?.status === 'success' ? n.result : undefined,
      version: v?.status === 'success' ? v.result : undefined,
      decimals: d?.status === 'success' ? d.result : undefined,
      domainSeparator,
    },
    tokenError: reads.error
      ? reads.error.message
      : s?.status === 'failure'
        ? `DOMAIN_SEPARATOR() eth_call failed: ${s.error.message}`
        : undefined,
    tokenLoading: reads.isLoading,
    separatorsMatch: domainSeparator && facSep ? domainSeparator.toLowerCase() === facSep.toLowerCase() : undefined,
  }
}
