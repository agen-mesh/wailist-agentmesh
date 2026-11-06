import { encodeAbiParameters, keccak256, toBytes, type Address, type Hex } from 'viem'

export const ARC_CHAIN_ID = 5042002
export const ARC_CAIP2 = 'eip155:5042002'
export const ARC_USDC: Address = '0x3600000000000000000000000000000000000000'

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

const DOMAIN_TYPE = 'EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)'

export function eip712DomainSeparator(domain: {
  name: string
  version: string
  chainId: number
  verifyingContract: Address
}): Hex {
  const typeHash = keccak256(toBytes(DOMAIN_TYPE))
  return keccak256(
    encodeAbiParameters(
      [{ type: 'bytes32' }, { type: 'bytes32' }, { type: 'bytes32' }, { type: 'uint256' }, { type: 'address' }],
      [
        typeHash,
        keccak256(toBytes(domain.name)),
        keccak256(toBytes(domain.version)),
        BigInt(domain.chainId),
        domain.verifyingContract,
      ],
    ),
  )
}

/**
 * Refuse to ask the wallet to sign unless the typed data is Arc Testnet USDC.
 * When the token's DOMAIN_SEPARATOR() has been read, the local hash must match
 * it too — a facilitator-supplied name/version that does not hash to the
 * token's separator would authorize a different contract.
 */
export function assertSignableArcDomain(
  domain: { chainId: number; verifyingContract: string },
  onchainSeparator: string | undefined,
  localSeparator: string | undefined,
): void {
  if (domain.chainId !== ARC_CHAIN_ID || domain.verifyingContract.toLowerCase() !== ARC_USDC.toLowerCase()) {
    throw new Error(
      `refusing to sign: domain is chain ${domain.chainId} contract ${domain.verifyingContract}, want Arc Testnet USDC (${ARC_CHAIN_ID} ${ARC_USDC})`,
    )
  }
  if (onchainSeparator && (!localSeparator || localSeparator.toLowerCase() !== onchainSeparator.toLowerCase())) {
    throw new Error('refusing to sign: EIP-712 domain hash does not match the token DOMAIN_SEPARATOR()')
  }
}
