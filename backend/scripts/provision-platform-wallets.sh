#!/usr/bin/env bash
# Interactively provisions the two mainnet platform wallets AgentMesh's
# backend needs at boot (PLATFORM_WALLET = Wallet 2 / payTo,
# PLATFORM_SPEND_WALLET = Wallet 1 / spend) and optionally pushes the
# resulting env vars straight to Railway.
#
# Generates fresh Algo25 wallets via ./cmd/walletgen rather than importing
# an existing Pera "Universal Wallet" (24-word BIP-39) account: this
# codebase's ENCRYPTION_KEY/encoding scheme only speaks Algo25, and there is
# no supported way to export a Universal Wallet account's key in that
# format. Fund the freshly generated addresses from your existing wallets
# with a normal transfer instead.
#
# Writes two local files at the end (both gitignored, never pushed):
#   scripts/wallets.env                    -- ciphertext only (addresses +
#                                              ENCRYPTION_KEY-encrypted
#                                              mnemonics). Useless without
#                                              ENCRYPTION_KEY, safe to share
#                                              with a cofounder/store long-term.
#   scripts/wallets.SECRET-mnemonics.txt   -- the raw 25-word phrases in
#                                              PLAINTEXT. A real, complete
#                                              private key backup sitting on
#                                              this disk -- treat it like
#                                              cash, not a normal file
#                                              (chmod 600'd automatically,
#                                              but that's not encryption).
#
# Run from anywhere; it cds to backend/ itself.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

ENV_FILE="scripts/wallets.env"
SECRET_FILE="scripts/wallets.SECRET-mnemonics.txt"
for f in "$ENV_FILE" "$SECRET_FILE"; do
  if [ -f "$f" ]; then
    backup="${f}.bak.$(date +%s)"
    mv "$f" "$backup"
    echo "Existing $f found -- moved to $backup rather than overwriting silently." >&2
  fi
done
: > "$SECRET_FILE"
chmod 600 "$SECRET_FILE"

echo "=== AgentMesh platform wallet provisioning ==="
echo

read -r -s -p "ENCRYPTION_KEY (must match the one already set on your deploy — hidden input): " ENC_KEY
echo
if [ -z "$ENC_KEY" ]; then
  echo "ENCRYPTION_KEY is required." >&2
  exit 1
fi

read -r -p "Network [mainnet]: " NETWORK
NETWORK=${NETWORK:-mainnet}

if [ "$NETWORK" = "mainnet" ]; then
  DEFAULT_ALGOD="https://mainnet-api.algonode.cloud"
  ASSET_ID=31566704
else
  DEFAULT_ALGOD="https://testnet-api.algonode.cloud"
  ASSET_ID=10458941
fi
read -r -p "Algod URL [$DEFAULT_ALGOD]: " ALGOD_URL
ALGOD_URL=${ALGOD_URL:-$DEFAULT_ALGOD}

declare -A ADDR
declare -A ENC

provision_one() {
  local var_prefix="$1" label="$2"
  echo
  echo "--------------------------------------------------------------"
  echo "Provisioning $var_prefix ($label)"
  echo "--------------------------------------------------------------"

  local full_output enc_mnemonic info address gen_status
  set +e
  full_output=$(go run ./cmd/walletgen \
    -enc-key="$ENC_KEY" -network="$NETWORK" -algod-url="$ALGOD_URL" \
    -skip-opt-in -show-mnemonic 2>&1)
  gen_status=$?
  set -e
  if [ "$gen_status" -ne 0 ]; then
    echo "walletgen exited with status $gen_status:" >&2
    printf '%s\n' "$full_output" >&2
    exit 1
  fi

  enc_mnemonic=$(printf '%s\n' "$full_output" | tail -n1)
  info=$(printf '%s\n' "$full_output" | head -n -1)
  address=$(printf '%s\n' "$info" | grep -oP 'Address: \K\S+' || true)

  if [ -z "$address" ] || [ -z "$enc_mnemonic" ]; then
    echo "walletgen did not produce an address/encrypted mnemonic — full output:" >&2
    printf '%s\n' "$full_output" >&2
    exit 1
  fi

  printf '%s\n' "$info"

  local raw_mnemonic
  raw_mnemonic=$(printf '%s\n' "$info" | sed -n '/^=== RAW MNEMONIC/,/^=== do not paste/p' | sed '1d;$d')
  {
    echo "=== $var_prefix ($label) ==="
    echo "Address: $address"
    echo "Mnemonic: $raw_mnemonic"
    echo
  } >> "$SECRET_FILE"

  echo
  echo ">>> Import the 25 words above into Pera now: Add Wallet > Import Account > Algo25 (legacy)."
  read -r -p "Press Enter once you've written it down / imported it... "

  echo
  echo ">>> Send at least 2 ALGO to $address (covers fees, min balance, USDC opt-in)."
  read -r -p "Press Enter once that ALGO transfer has confirmed... "

  echo "Opting $var_prefix into USDC (asset $ASSET_ID)..."
  go run ./cmd/walletgen \
    -enc-key="$ENC_KEY" -network="$NETWORK" -algod-url="$ALGOD_URL" \
    -asset-id="$ASSET_ID" -opt-in-only="$enc_mnemonic"

  echo
  echo ">>> Now send USDC to $address from your existing wallet (can also do this later)."
  read -r -p "Press Enter once sent, or just Enter to skip for now... "

  ADDR[$var_prefix]=$address
  ENC[$var_prefix]=$enc_mnemonic
}

provision_one PLATFORM_WALLET "Wallet 2 — payTo, receives inbound settlements"
provision_one PLATFORM_SPEND_WALLET "Wallet 1 — spend, pays outbound on behalf of user credits"

{
  echo "ALGORAND_NETWORK=$NETWORK"
  echo "ALGOD_URL=$ALGOD_URL"
  echo "PLATFORM_WALLET_ADDRESS=${ADDR[PLATFORM_WALLET]}"
  echo "PLATFORM_WALLET_ENC_MNEMONIC=${ENC[PLATFORM_WALLET]}"
  echo "PLATFORM_SPEND_WALLET_ADDRESS=${ADDR[PLATFORM_SPEND_WALLET]}"
  echo "PLATFORM_SPEND_WALLET_ENC_MNEMONIC=${ENC[PLATFORM_SPEND_WALLET]}"
} > "$ENV_FILE"
chmod 600 "$ENV_FILE"

echo
echo "================================================================"
echo "Done. Env vars for your deploy (ciphertext only — safe to store/share):"
echo "================================================================"
cat "$ENV_FILE"
echo
echo "Written to: $(pwd)/$ENV_FILE"
echo "  -> ciphertext only, useless without ENCRYPTION_KEY. Safe to share with"
echo "     your cofounder / keep for your own records. Gitignored -- will"
echo "     never get committed or pushed."
echo
echo "Raw 25-word mnemonics written to: $(pwd)/$SECRET_FILE"
echo "  -> PLAINTEXT private keys. chmod 600'd, gitignored, but that is NOT"
echo "     encryption -- treat this file like a physical key. Move it"
echo "     somewhere durable and offline (or a password manager) and delete"
echo "     the copy on this machine once you have; don't let it linger here"
echo "     or sync into a cloud-backed folder."
echo

read -r -p "Push these 6 vars to Railway now via 'railway variable set' (--skip-deploys, no redeploy triggered)? [y/N] " PUSH
if [ "$PUSH" = "y" ] || [ "$PUSH" = "Y" ]; then
  read -r -p "Railway service name [wailist-agentmesh]: " SVC
  SVC=${SVC:-wailist-agentmesh}
  read -r -p "Railway environment [production]: " ENVIRONMENT
  ENVIRONMENT=${ENVIRONMENT:-production}

  railway variable set "ALGORAND_NETWORK=$NETWORK" --service "$SVC" --environment "$ENVIRONMENT" --skip-deploys
  railway variable set "ALGOD_URL=$ALGOD_URL" --service "$SVC" --environment "$ENVIRONMENT" --skip-deploys
  railway variable set "PLATFORM_WALLET_ADDRESS=${ADDR[PLATFORM_WALLET]}" --service "$SVC" --environment "$ENVIRONMENT" --skip-deploys
  railway variable set "PLATFORM_WALLET_ENC_MNEMONIC=${ENC[PLATFORM_WALLET]}" --service "$SVC" --environment "$ENVIRONMENT" --skip-deploys
  railway variable set "PLATFORM_SPEND_WALLET_ADDRESS=${ADDR[PLATFORM_SPEND_WALLET]}" --service "$SVC" --environment "$ENVIRONMENT" --skip-deploys
  railway variable set "PLATFORM_SPEND_WALLET_ENC_MNEMONIC=${ENC[PLATFORM_SPEND_WALLET]}" --service "$SVC" --environment "$ENVIRONMENT" --skip-deploys

  echo
  echo "Vars set (deploy NOT triggered — deploy manually once you've verified the current"
  echo "deployment's health, since it was showing a failed status before this ran)."
else
  echo "Skipped Railway push. Set the 6 vars above manually when ready."
fi
