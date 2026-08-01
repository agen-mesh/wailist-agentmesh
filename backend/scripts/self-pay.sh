#!/usr/bin/env bash
# self-pay.sh — Bootstrap the Bazaar discovery catalog by making one real
# payment against the bare /x402/relay endpoint. The GoPlausible facilitator
# only creates a catalog entry AFTER a client successfully pays, so this
# script is the missing trigger.
#
# Prerequisites:
#   - Backend must be deployed and reachable at BACKEND_URL
#   - Platform spend wallet (PLATFORM_SPEND_WALLET_ADDRESS) must hold USDC
#   - FACILITATOR_URL defaults to https://facilitator.goplausible.xyz
#
# Usage:
#   export BACKEND_URL=https://your-railway-url.up.railway.app
#   ./scripts/self-pay.sh          # Step 1: inspect the 402 challenge
#   ./scripts/self-pay.sh verify   # Step 2: check if you're in the Bazaar

set -euo pipefail

BACKEND_URL="${BACKEND_URL:-https://wailist-agentmesh-production.up.railway.app}"
FACILITATOR_URL="${FACILITATOR_URL:-https://facilitator.goplausible.xyz}"
PLATFORM_PAYTO="${PLATFORM_PAYTO:-XJAS6HBWDZB4JW3SQB7GBP7BY3C2I43SGL5WI2KCZHLIUFW5HO4FPCVAW4}"

echo "=========================================="
echo "AgentMesh — Bazaar Discovery Bootstrap"
echo "=========================================="
echo ""

if [[ "${1:-}" == "verify" ]]; then
    echo "▶ Checking Bazaar discovery for AgentMesh..."
    echo ""

    echo "── Step 1: Check /discovery/resources ──"
    RESOURCES=$(curl -s "${FACILITATOR_URL}/discovery/resources?includeTestnets=true&limit=1000")
    MATCH=$(echo "$RESOURCES" | python3 -c "
import sys, json
data = json.load(sys.stdin)
items = [i for i in data.get('items', []) if 'agent-mesh' in str(i).lower() or 'agentmesh' in str(i).lower()]
if items:
    print(json.dumps(items, indent=2))
else:
    print('NOT FOUND')
" 2>/dev/null || echo "PARSE ERROR")
    echo "$MATCH"
    echo ""

    echo "── Step 2: Check /discovery/merchants ──"
    MERCHANTS=$(curl -s "${FACILITATOR_URL}/discovery/merchants?includeTestnets=true&limit=500")
    MATCH=$(echo "$MERCHANTS" | python3 -c "
import sys, json
data = json.load(sys.stdin)
items = [i for i in data.get('items', []) if '${PLATFORM_PAYTO}' in str(i) or 'agent-mesh' in str(i).lower()]
if items:
    print(json.dumps(items, indent=2))
else:
    print('NOT FOUND')
" 2>/dev/null || echo "PARSE ERROR")
    echo "$MATCH"
    echo ""

    echo "── Step 3: Compute expected merchantId ──"
    echo -n "$PLATFORM_PAYTO" | cut -c1-24 | tr -d '\n' | base64
    echo ""
    echo ""
    echo "Done. If NOT FOUND, the self-payment hasn't settled yet."
    exit 0
fi

echo "▶ Step 1: Fetch the 402 challenge from bare /x402/relay"
echo "  URL: ${BACKEND_URL}/x402/relay"
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" "${BACKEND_URL}/x402/relay")
HTTP_CODE=$(echo "$RESPONSE" | tail -1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [[ "$HTTP_CODE" == "402" ]]; then
    echo "✅ Got 402 Payment Required — endpoint is live!"
    echo ""
    echo "Challenge body (truncated):"
    echo "$BODY" | python3 -m json.tool 2>/dev/null | head -30 || echo "$BODY" | head -5
    echo ""
    echo "── Key fields ──"
    echo "$BODY" | python3 -c "
import sys, json
d = json.load(sys.stdin)
print(f'  x402Version: {d.get(\"x402Version\")}')
r = d.get('resource', {})
print(f'  resource.url: {r.get(\"url\")}')
print(f'  resource.serviceName: {r.get(\"serviceName\")}')
print(f'  resource.tags: {r.get(\"tags\")}')
a = d.get('accepts', [{}])[0]
print(f'  payTo: {a.get(\"payTo\")}')
print(f'  amount: {a.get(\"amount\")}')
print(f'  network: {a.get(\"network\")}')
e = a.get('extra', {})
print(f'  extra.tag: {e.get(\"tag\")}')
print(f'  extra.feePayer: {e.get(\"feePayer\")}')
ext = d.get('extensions', {}).get('bazaar', {})
print(f'  extensions.bazaar: {\"present\" if ext else \"MISSING\"}')
" 2>/dev/null || echo "  (could not parse)"
    echo ""
    echo "══════════════════════════════════════════"
    echo "The 402 challenge looks correct."
    echo ""
    echo "NEXT STEP: Make one real payment against this endpoint."
    echo "You can use the @x402/avm SDK or any x402 client to pay"
    echo "\$0.01 USDC to ${PLATFORM_PAYTO} on the network shown above."
    echo ""
    echo "After payment settles, run:"
    echo "  ./scripts/self-pay.sh verify"
    echo "══════════════════════════════════════════"
elif [[ "$HTTP_CODE" == "404" ]]; then
    echo "❌ Got 404 — backend is not deployed at this URL."
    echo "   Check your Railway deployment or set BACKEND_URL."
    exit 1
else
    echo "⚠️  Got HTTP $HTTP_CODE (expected 402)"
    echo "   Body: $BODY"
    exit 1
fi
