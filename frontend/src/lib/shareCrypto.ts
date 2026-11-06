const fragmentMessage =
  "This private share needs the #k= key from the original link.";

export class ShareKeyError extends Error {
  constructor() {
    super(fragmentMessage);
    this.name = "ShareKeyError";
  }
}

function bytesToBase64Url(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function base64UrlToBytes(value: string): Uint8Array | null {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) return null;
  const padded = value + "=".repeat((4 - (value.length % 4)) % 4);
  try {
    const binary = atob(padded.replace(/-/g, "+").replace(/_/g, "/"));
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return bytes;
  } catch {
    return null;
  }
}

export async function sealShare(
  plain: Uint8Array,
): Promise<{ ciphertext: Uint8Array; key: string }> {
  const keyBytes = crypto.getRandomValues(new Uint8Array(32));
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const cryptoKey = await crypto.subtle.importKey(
    "raw",
    keyBytes as BufferSource,
    "AES-GCM",
    false,
    ["encrypt"],
  );
  const sealed = new Uint8Array(
    await crypto.subtle.encrypt(
      { name: "AES-GCM", iv: iv as BufferSource, additionalData: new Uint8Array(0) },
      cryptoKey,
      plain as BufferSource,
    ),
  );
  const ciphertext = new Uint8Array(iv.byteLength + sealed.byteLength);
  ciphertext.set(iv, 0);
  ciphertext.set(sealed, iv.byteLength);
  return { ciphertext, key: bytesToBase64Url(keyBytes) };
}

export async function openShare(
  ciphertext: Uint8Array,
  key: string,
): Promise<Uint8Array> {
  const keyBytes = base64UrlToBytes(key);
  if (!keyBytes || keyBytes.byteLength !== 32 || ciphertext.byteLength < 28) {
    throw new ShareKeyError();
  }
  try {
    const cryptoKey = await crypto.subtle.importKey(
      "raw",
      keyBytes as BufferSource,
      "AES-GCM",
      false,
      ["decrypt"],
    );
    const plain = await crypto.subtle.decrypt(
      {
        name: "AES-GCM",
        iv: ciphertext.subarray(0, 12) as BufferSource,
        additionalData: new Uint8Array(0),
      },
      cryptoKey,
      ciphertext.subarray(12) as BufferSource,
    );
    return new Uint8Array(plain);
  } catch {
    throw new ShareKeyError();
  }
}
