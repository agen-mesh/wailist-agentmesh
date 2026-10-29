"use client";

import type { PurchaseConfig, PurchaseOrder } from "@/lib/purchase";
import { expired, receiptURL, usdc } from "./format";
import styles from "./Purchases.module.css";

const STATUS: Record<string, { title: string; detail: string }> = {
  awaiting_identity: { title: "Fresh verification needed", detail: "Verify through Agent Pass, then review and approve this exact purchase." },
  awaiting_approval: { title: "Your decision is needed", detail: "Verification is complete. No purchase is approved until you choose Approve." },
  approved: { title: "Approved, awaiting your wallet", detail: "Sign only the displayed amount, recipient and deadline." },
  authorization_released: { title: "Authorization prepared", detail: "The authorization may be usable until its deadline. Reconcile this order before taking another payment action." },
  submitting: { title: "Payment submission pending", detail: "Keep this order. A delayed response does not mean the payment failed." },
  payment_unknown: { title: "Payment outcome unresolved", detail: "The server must reconcile the original authorization and transfer. Do not create a replacement payment." },
  paid: { title: "Payment verified", detail: "The service result is not ready yet. No second payment is needed." },
  fulfilling: { title: "Service is running", detail: "The merchant is fulfilling this paid order. Reloading retrieves the same order." },
  completed: { title: "Service complete", detail: "The result and payment evidence belong to this order." },
  paid_tool_failed: { title: "Paid, service failed", detail: "Payment remains recorded. Recovery depends on the merchant; no automatic refund is promised." },
  fulfillment_unknown: { title: "Service outcome unresolved", detail: "Payment is recorded, but the result needs reconciliation. Do not run the service again blindly." },
  denied: { title: "Purchase denied", detail: "This order was denied before payment authorization." },
  expired: { title: "Order expired", detail: "This order cannot authorize a new payment. Any released authorization still needs reconciliation." },
  cancelled: { title: "Unsigned order cancelled", detail: "The server cancelled this order before releasing payment authorization." },
};

export interface OrderPanelProps {
  order: PurchaseOrder;
  config: PurchaseConfig | null;
  account: string;
  busy: boolean;
  stale: boolean;
  now: number;
  onIdentity: () => void;
  onDecision: (decision: "approve" | "deny") => void;
  onSign: () => void;
  onReconcile: () => void;
  onCancel: () => void;
  onRefresh: () => void;
}

export function OrderPanel({ order, config, account, busy, stale, now, onIdentity, onDecision, onSign, onReconcile, onCancel, onRefresh }: OrderPanelProps) {
  const status = STATUS[order.state] ?? { title: "Order status unavailable", detail: "Refresh the order before taking any action." };
  const released = Boolean(order.signatureReleasedAt);
  const pastDeadline = expired(order.deadline, now);
  const disabled = busy || stale || !config?.enabled;
  const walletMatches = Boolean(account) && account.toLowerCase() === order.payer.toLowerCase();
  const link = receiptURL(order);
  const paymentVerified = order.paymentEvent?.chainId === order.chainId && order.paymentEvent?.txHash === order.txHash;
  const identityStep = !released && (order.state === "awaiting_identity" || order.state === "awaiting_approval");

  return (
    <section className={styles.card} aria-label="Exact purchase order" aria-busy={busy}>
      <div className={styles.row}><span className={styles.badge}>ORDER</span><span className={styles.badge}>{order.state.replaceAll("_", " ")}</span></div>
      <h2 style={{ marginTop: 14 }}>{status.title}</h2>
      <p className={styles.muted} role="status">{status.detail}</p>
      {stale && <p className={styles.warning}>Current status could not be confirmed. Payment actions are disabled until a refresh succeeds.</p>}
      {pastDeadline && !released && <p className={styles.warning}>The purchase deadline has passed. Request a new offer and review a new order.</p>}
      {released && <p className={styles.warning}>A released signature cannot be revoked by closing this page or cancelling in the app. Check the original payment outcome.</p>}
      {order.reason && <p className={styles.notice}>{order.reason}</p>}

      <dl className={styles.facts}>
        <dt>Service</dt><dd>{order.offer.capability}</dd>
        <dt>Exact payment</dt><dd><strong>{usdc(order.amountMicros)} USDC</strong></dd>
        <dt>Maximum approved cost</dt><dd>{usdc(order.maxMicros)} USDC</dd>
        <dt>Customer wallet</dt><dd className={styles.mono}>{order.payer}</dd>
        <dt>Payee</dt><dd className={styles.mono}>{order.payee}</dd>
        <dt>Settlement network</dt><dd>Arc testnet, chain {order.chainId}</dd>
        <dt>Token</dt><dd className={styles.mono}>{order.asset}</dd>
        <dt>Deadline (UTC)</dt><dd><time dateTime={order.deadline}>{order.deadline}</time></dd>
        <dt>Service request</dt><dd className={styles.mono}>{order.method} {order.resource}</dd>
        <dt>Order ID</dt><dd className={styles.mono}>{order.id}</dd>
      </dl>
      <details className={styles.details}>
        <summary>Inspect immutable purchase terms</summary>
        <dl className={styles.facts}>
          <dt>Offer key</dt><dd className={styles.mono}>{order.offer.key}</dd>
          <dt>Offer owner</dt><dd className={styles.mono}>{order.offer.owner}</dd>
          <dt>Offer version</dt><dd>{order.offer.version}</dd>
          <dt>Offer content</dt><dd className={styles.mono}>{order.offer.contentHash}</dd>
          <dt>Offer snapshot</dt><dd className={styles.mono}>{order.offer.snapshotHash}</dd>
          <dt>Request digest</dt><dd className={styles.mono}>{order.requestDigest}</dd>
          <dt>Quote digest</dt><dd className={styles.mono}>{order.quoteDigest}</dd>
          <dt>Intent / payment nonce</dt><dd className={styles.mono}>{order.intentHash}</dd>
          <dt>Arkiv expiry block</dt><dd>{order.offer.expiresAtBlock}</dd>
          <dt>Last observed block</dt><dd>{order.offer.observedBlock}</dd>
        </dl>
      </details>

      {!released && !["denied", "cancelled", "expired", "completed"].includes(order.state) && <>
        <div className={styles.step}>
          <h3>1. Fresh human verification</h3>
          <p className={styles.muted}>{order.identityVerifiedAt ? `Verified at ${order.identityVerifiedAt}. The server checks freshness before approval.` : "Agent Pass must verify this order's account and intent."} Verification does not prove wallet ownership or approve a payment.</p>
          {identityStep && <button className={styles.secondary} disabled={disabled || pastDeadline || !config?.worldAvailable} onClick={onIdentity}>Verify with Agent Pass</button>}
        </div>
        <div className={styles.step}>
          <h3>2. Approve this exact purchase</h3>
          {order.approvedAt ? <p className={styles.muted}>Approved at {order.approvedAt}.</p> : <p className={styles.muted}>Check the service, destination and capped cost above. This decision is separate from human verification.</p>}
          {!order.approvedAt && <div className={styles.row}>
            <button className={styles.button} disabled={disabled || pastDeadline || !order.canApprove} onClick={() => onDecision("approve")}>Approve {usdc(order.amountMicros)} USDC</button>
            <button className={styles.danger} disabled={disabled || !order.canApprove} onClick={() => onDecision("deny")}>Deny purchase</button>
          </div>}
        </div>
        <div className={styles.step}>
          <h3>3. Sign the payment authorization</h3>
          <p className={styles.muted}>Your linked EOA wallet authorizes the exact USDC transfer. The relayer pays network gas. This does not use application credits.</p>
          {!walletMatches && <p className={styles.notice}>Connect the customer wallet shown in this order before signing.</p>}
          <button className={styles.button} disabled={disabled || pastDeadline || !walletMatches || !order.canAuthorize} onClick={onSign}>Sign exact payment</button>
        </div>
      </>}

      {order.state === "completed" && <div className={styles.step}>
        <h3>Service result</h3>
        <pre className={styles.result} tabIndex={0}>{typeof order.result === "string" ? order.result : JSON.stringify(order.result ?? null, null, 2)}</pre>
      </div>}
      {(order.txHash || order.paymentEvent) && <div className={styles.step}>
        <h3>{paymentVerified ? "Verified payment receipt" : "Transaction awaiting verification"}</h3>
        <p className={styles.mono}>{order.txHash}</p>
        {paymentVerified && <p className={styles.muted}>Block {order.paymentEvent?.blockNumber}, transfer log {order.paymentEvent?.logIndex}. Verification is reported by the server.</p>}
        {link && config?.evidenceMode === "live" && <a className={styles.link} href={link} target="_blank" rel="noopener noreferrer">View transaction on Arc explorer</a>}
        {config?.evidenceMode === "simulation" && <p className={styles.warning}>Simulated receipt. This transaction is not presented as a live Arc transfer.</p>}
      </div>}
      <div className={`${styles.row} ${styles.step}`}>
        <button className={styles.secondary} disabled={busy} onClick={onRefresh}>Refresh order</button>
        {order.canReconcile && <button className={styles.button} disabled={busy || stale} onClick={onReconcile}>Reconcile original order</button>}
        {order.canCancel && !released && <button className={styles.secondary} disabled={disabled} onClick={onCancel}>Cancel unsigned order</button>}
      </div>
    </section>
  );
}
