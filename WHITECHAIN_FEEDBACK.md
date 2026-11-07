# Whitechain feedback

What worked: a workflow payment in the real USDT contract, not a token we minted. Wallet 2 `0x26B70253feE141dEE4ece1E5efBe2cf70EAe26fc` sent 0.01 USDT with `transferWithAuthorization` to `0xDBA5141562B65De3F7D3afefE156AEC46B87f8c4`. The facilitator submitted it. Success, block 12422351:

https://explorer.testnet.whitechain.io/tx/0xbddeb9a9a3344b3ebe449e1108f37ce306d7c34795b6a2eed0e7b812e0675f94

The EIP-712 domain that verifies is name `Tether USD`, version `1`, chain `1874`, verifying contract `0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7`. The token has no `version()` getter. The version had to be taken from the facilitator's `/assets` and checked against `DOMAIN_SEPARATOR()`. That check is what made the signature settle.

Where we got stuck:

- There is no public USDT faucet. WBT has one (`https://faucet.testnet.whitechain.io`). USDT does not. Only the token owner can mint `0xcedfB56113143d55b2358b8CFbB0dEB5Ad2f55a7`, so a new hackathon wallet cannot pay until someone sends it USDT. The docs should say that next to the faucet, in the same sentence.
- Test USDT and test WBT have no monetary value. The payment above is a testnet settlement. It is the flow we ship. It is not a mainnet payment.
- The facilitator's error when the signed `from` has no USDT is `invalid_exact_evm_insufficient_balance`. That is the payer in the signature, which for us is Wallet 2, not whatever account is open in a browser. A one-line note on `/verify` saying which address was short would have saved a round of looking at the wrong wallet.

What the docs were missing: a single page with chain id `1874`, the USDT address, the EIP-712 name and version, the facilitator origin, and the fact that the relayer pays WBT so the payer does not need gas. Those values are scattered, and the version is the one that is easy to get wrong because the contract will not tell you.
