#!/usr/bin/env node
import { createWalletClient, ExpirationTime } from "@arkiv-network/sdk";
import { rabdi } from "@arkiv-network/sdk/chains";
import { http } from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { buildCreateArgs, shareSchema } from "./shareSchema.mjs";

const key = process.env.ARKIV_SHARE_PRIVATE_KEY ?? "";
if (!/^0x[0-9a-fA-F]{64}$/.test(key)) {
  process.stderr.write("share_disabled");
  process.exit(1);
}

const chunks = [];
for await (const chunk of process.stdin) chunks.push(chunk);
const payload = new Uint8Array(Buffer.concat(chunks));
const args = buildCreateArgs(payload);

try {
  const account = privateKeyToAccount(key);
  const client = createWalletClient({
    account,
    chain: rabdi({ accessKey: process.env.ARKIV_ACCESS_KEY ?? "" }),
    transport: http(),
  });
  const created = await client.createEntity(
    shareSchema,
    { payload: args.values.payload },
    {
      expiration: ExpirationTime.fromDays(args.expirationDays),
      flags: { readonly: args.readonly },
    },
  );
  process.stdout.write(
    JSON.stringify({ entityKey: created.entityKey, txHash: created.txHash }) + "\n",
  );
} catch {
  process.stderr.write("share_unconfirmed");
  process.exit(1);
}
