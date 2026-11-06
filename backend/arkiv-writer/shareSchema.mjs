import { bytes, defineSchema } from "@arkiv-network/sdk/schema";

export const shareSchema = defineSchema("agentmesh.share.v1", {
  payload: bytes(),
});

export function buildCreateArgs(payload) {
  return {
    schemaName: "agentmesh.share.v1",
    values: { payload },
    expirationDays: 7,
    readonly: true,
  };
}
