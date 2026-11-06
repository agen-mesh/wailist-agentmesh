# walletgen

Provisions a Whitechain (EVM) key for one of AgentMesh's platform wallets and
prints its address plus an `ENCRYPTION_KEY`-encrypted private key. Run this
locally. Never commit the output: stdout is a secret.

The server checks that each encrypted key derives the address in the matching
`PLATFORM_*_ADDRESS` variable, and refuses to start if it does not.

```bash
# Fresh key
go run ./cmd/walletgen -enc-key "$ENCRYPTION_KEY"

# Encrypt a key you already have. Pass "-" to read one line from stdin
# (the private keys live in ~/.agentmesh/whitechain-wallets.txt, mode 600).
go run ./cmd/walletgen -enc-key "$ENCRYPTION_KEY" -import -
```

Copy `ENC_KEY` into `PLATFORM_SPEND_WALLET_ENC_KEY` (Wallet 1, the payer) or
`PLATFORM_WALLET_ENC_KEY` (Wallet 2, `payTo` and outbound tools). Clear the
terminal scrollback afterwards.

Neither wallet needs WBT for x402. They only sign EIP-3009 authorizations,
and the facilitator pays the gas. They need testnet USDT. There is no public
USDT faucet.
