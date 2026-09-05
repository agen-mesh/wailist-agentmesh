// The bridge to our own Android secure-store plugin.
//
// Registered by hand rather than imported as a package because the plugin
// lives in the app module (android/app/src/main/java/ai/agentmesh/app), not in
// node_modules -- the same arrangement as nativeGeofence.ts, for the same
// reason.
//
// It wraps EncryptedSharedPreferences: the file on disk is AES-GCM ciphertext
// and the key that opens it is held in the Android Keystore. See
// SecureStorePlugin.java for what that does and does not protect against.
import { registerPlugin } from "@capacitor/core";

export interface SecureStorePlugin {
  /** Absent keys resolve with value: null. Missing is not an error. */
  get(options: { key: string }): Promise<{ value: string | null }>;
  set(options: { key: string; value: string }): Promise<void>;
  remove(options: { key: string }): Promise<void>;
}

export const SecureStore = registerPlugin<SecureStorePlugin>("SecureStore");
