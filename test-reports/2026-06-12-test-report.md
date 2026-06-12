# AgentMesh Backend — Test Report
**Date:** 2026-06-12 02:15
**Database:** postgres://localhost:5432/agentmesh (Docker, fresh)

## Summary

| | |
|-|-|
| **Total tests** | 68 |
| ✅ Passed | 68 |
| ❌ Failed | 0 |
| ⏭ Skipped | 0 |
| 🆕 New tests (this session) | 17 |

## Results by Package

### `internal/api`
✅ 3 passed

| Test | Status | ms | New |
|------|--------|----|-----|
| `TestHealthCheck` | ✅ | 0 |  |
| `TestNewAuthMiddlewareAcceptsValidToken` | ✅ | 0 |  |
| `TestNewAuthMiddlewareRejectsNoToken` | ✅ | 0 |  |

### `internal/api/handlers`
✅ 27 passed

| Test | Status | ms | New |
|------|--------|----|-----|
| `TestAPIKeyEncryption` | ✅ | 10 |  |
| `TestCreateAndGetWorkflow` | ✅ | 10 |  |
| `TestDecryptNodes_DecryptsEncryptedFields` | ✅ | 0 |  |
| `TestDecryptNodes_DoesNotMutateOriginal` | ✅ | 0 |  |
| `TestDecryptNodes_EmptyKeyIsNoOp` | ✅ | 0 |  |
| `TestDeploy` | ✅ | 10 |  |
| `TestDeployGeneratesWebhookURL` | ✅ | 30 | 🆕 |
| `TestDeployNonWebhookTriggerNoURL` | ✅ | 10 | 🆕 |
| `TestDeployPlatformWalletUsedWhenEnvSet` | ✅ | 10 | 🆕 |
| `TestDeploySelfFundWalletGeneratesNewWallet` | ✅ | 10 | 🆕 |
| `TestDeployWebhookRespectsCustomMethod` | ✅ | 10 | 🆕 |
| `TestEncryptDecryptRoundTrip` | ✅ | 0 |  |
| `TestEncryptField_AlreadyEncryptedPassesThrough` | ✅ | 0 |  |
| `TestEncryptField_EmptyEncryptionKeyIsNoOp` | ✅ | 0 |  |
| `TestEncryptField_EmptyPreservesExisting` | ✅ | 0 |  |
| `TestEncryptField_PlaintextGetsEncrypted` | ✅ | 0 |  |
| `TestEncryptField_SentinelPreservesExisting` | ✅ | 0 |  |
| `TestEncryptNodes_NewNodeWithSentinelHasNoKey` | ✅ | 0 |  |
| `TestEncryptNodes_SentinelPreservesExistingEncryptedBlob` | ✅ | 0 |  |
| `TestMaskNodes_DoesNotMutateOriginal` | ✅ | 0 |  |
| `TestMaskNodes_EncryptedFieldsGetSentinel` | ✅ | 0 |  |
| `TestSignUpReturnsBadRequestOnEmptyEmail` | ✅ | 0 |  |
| `TestSignUpReturnsBadRequestOnShortPassword` | ✅ | 0 |  |
| `TestStopWorkflowNoActiveRun` | ✅ | 10 |  |
| `TestStopWorkflowNotFound` | ✅ | 10 |  |
| `TestStopWorkflowWrongUser` | ✅ | 10 |  |
| `TestTriggerRun` | ✅ | 10 |  |

### `internal/db`
✅ 4 passed

| Test | Status | ms | New |
|------|--------|----|-----|
| `TestAgentWallet` | ✅ | 20 |  |
| `TestConnect` | ✅ | 40 |  |
| `TestRunAndLogs` | ✅ | 20 |  |
| `TestWorkflowCRUD` | ✅ | 20 |  |

### `internal/engine`
✅ 6 passed

| Test | Status | ms | New |
|------|--------|----|-----|
| `TestBuildAttachMap` | ✅ | 0 |  |
| `TestCycleDetected` | ✅ | 0 |  |
| `TestStopReturnsFalseWhenNotRunning` | ✅ | 20 |  |
| `TestStopReturnsTrueImmediatelyAfterStart` | ✅ | 20 |  |
| `TestStopSetsRunStatusStopped` | ✅ | 40 |  |
| `TestTopologicalSort` | ✅ | 0 |  |

### `internal/engine/nodes`
✅ 18 passed

| Test | Status | ms | New |
|------|--------|----|-----|
| `TestCalculator` | ✅ | 0 |  |
| `TestDatetime` | ✅ | 0 |  |
| `TestEmailEmptyKeyFallsToPlatform` | ✅ | 0 | 🆕 |
| `TestEmailSkipsWhenNoPlatformKey` | ✅ | 0 | 🆕 |
| `TestEmailUsesNodeKey` | ✅ | 0 | 🆕 |
| `TestEmailUsesPlatformKey` | ✅ | 0 | 🆕 |
| `TestExecuteAgentOpenAI` | ✅ | 0 |  |
| `TestHTTPTool` | ✅ | 0 |  |
| `TestLogAction` | ✅ | 0 |  |
| `TestResolveAPIKeyEmptyNodeKeyFallback` | ✅ | 0 | 🆕 |
| `TestResolveAPIKeyOwnKey` | ✅ | 0 | 🆕 |
| `TestResolveAPIKeyPlatformFallback` | ✅ | 0 | 🆕 |
| `TestWebhookAction` | ✅ | 0 |  |
| `TestX402FreeEndpoint` | ✅ | 0 |  |
| `TestX402NoWallet` | ✅ | 0 |  |
| `TestX402ParseQuote` | ✅ | 0 |  |
| `TestX402PaymentSigned` | ✅ | 0 |  |
| `TestX402SignerError` | ✅ | 0 |  |

### `internal/models`
✅ 2 passed

| Test | Status | ms | New |
|------|--------|----|-----|
| `TestAgentWalletMnemonicNotSerialized` | ✅ | 0 |  |
| `TestWorkflowGraphRoundtrip` | ✅ | 0 |  |

### `internal/sse`
✅ 1 passed

| Test | Status | ms | New |
|------|--------|----|-----|
| `TestBrokerPublishSubscribe` | ✅ | 0 |  |

### `internal/wallet`
✅ 7 passed

| Test | Status | ms | New |
|------|--------|----|-----|
| `TestEncryptDecrypt` | ✅ | 0 |  |
| `TestGenerateWallet` | ✅ | 0 |  |
| `TestWrapMnemonicAddressLength` | ✅ | 0 | 🆕 |
| `TestWrapMnemonicDecryptedIs25Words` | ✅ | 0 | 🆕 |
| `TestWrapMnemonicDifferentEncKey` | ✅ | 0 | 🆕 |
| `TestWrapMnemonicRejectsInvalid` | ✅ | 0 | 🆕 |
| `TestWrapMnemonicRoundtrip` | ✅ | 0 | 🆕 |

## 🆕 New Tests Added This Session

### Provider key resolution (`internal/engine/nodes`)
- **`TestResolveAPIKeyOwnKey`** — Node key used when UseOurKey=false
- **`TestResolveAPIKeyPlatformFallback`** — Env key used when UseOurKey=true
- **`TestResolveAPIKeyEmptyNodeKeyFallback`** — Empty node key falls back to env var

### Email key resolution (`internal/engine/nodes`)
- **`TestEmailUsesNodeKey`** — Node Resend key used when UseOurEmail=false
- **`TestEmailUsesPlatformKey`** — PLATFORM_RESEND_KEY used when UseOurEmail=true
- **`TestEmailEmptyKeyFallsToPlatform`** — Empty node key falls back to PLATFORM_RESEND_KEY
- **`TestEmailSkipsWhenNoPlatformKey`** — Graceful skip when no key at all

### Wallet WrapMnemonic (`internal/wallet`)
- **`TestWrapMnemonicRoundtrip`** — WrapMnemonic recovers correct address from mnemonic
- **`TestWrapMnemonicAddressLength`** — Address is 58 chars (Algorand standard)
- **`TestWrapMnemonicDecryptedIs25Words`** — Encrypted payload decrypts to valid 25-word mnemonic
- **`TestWrapMnemonicRejectsInvalid`** — Invalid mnemonic returns an error
- **`TestWrapMnemonicDifferentEncKey`** — Same mnemonic + different enc key = different ciphertext

### Deploy handler (`internal/api/handlers`)
- **`TestDeployGeneratesWebhookURL`** — Webhook trigger gets /hooks/{wfId}/{nodeId} URL on deploy
- **`TestDeployWebhookRespectsCustomMethod`** — Custom webhook method (GET) is preserved
- **`TestDeployNonWebhookTriggerNoURL`** — Cron/chat triggers produce no webhook URL
- **`TestDeployPlatformWalletUsedWhenEnvSet`** — PLATFORM_ALGO_MNEMONIC used for SelfFundWallet=false agents
- **`TestDeploySelfFundWalletGeneratesNewWallet`** — SelfFundWallet=true agents get a fresh keypair

---
*Report generated by `go test ./... -json` against a fresh Docker postgres instance.*