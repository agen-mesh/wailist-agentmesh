package ai.agentmesh.app;

import android.content.Context;
import android.content.SharedPreferences;

import androidx.security.crypto.EncryptedSharedPreferences;
import androidx.security.crypto.MasterKeys;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

import java.io.File;
import java.io.IOException;
import java.security.GeneralSecurityException;

/**
 * A small key-value store whose file is encrypted with a key held in the
 * Android Keystore.
 *
 * This exists because the session token was kept in @capacitor/preferences,
 * which on Android is plain SharedPreferences: app-private, but written to
 * disk in the clear. App-private is enough against another app; it is not
 * enough against anyone holding the device's filesystem -- a rooted phone, or
 * an image of one. android:allowBackup="false" already closed the backup
 * route, but it encrypted nothing.
 *
 * Written here rather than pulled in as a package. The plugin the plan named,
 * @capawesome-team/capacitor-secure-preferences, is published only to
 * Capawesome's sponsor registry and 404s on npm, so it was never installable.
 * Of the remaining options this is the one that adds no third-party code to
 * the path holding the session: the app already owns a native plugin
 * (GeofencePlugin) and registers it the same way, so the pattern costs nothing
 * new to carry.
 *
 * What this DOES guarantee: the bytes on disk are AES-GCM ciphertext, and the
 * key that decrypts them lives in the Keystore, which on a device with a
 * hardware-backed implementation never lets the key material into app memory
 * at all.
 *
 * What it does NOT guarantee, and the comment in native/auth.ts must not
 * claim: protection from an attacker running code AS this app on an unlocked
 * device. Anything the app can decrypt, code with the app's identity can also
 * decrypt. This raises the cost of an offline filesystem attack; it is not a
 * defence against a compromised runtime.
 */
@CapacitorPlugin(name = "SecureStore")
public class SecureStorePlugin extends Plugin {

    // Separate from any other preferences file the app keeps, so clearing a
    // corrupt store below cannot take unrelated state with it.
    private static final String FILE_NAME = "agentmesh_secure_store";

    private SharedPreferences prefs;

    /**
     * Opens the encrypted store, recreating it once if it cannot be read.
     *
     * The recovery path is not defensive padding. A Keystore entry can become
     * permanently undecryptable in ordinary use -- the user changes their lock
     * screen or biometrics, the app is restored onto a different device, or the
     * key is invalidated by a system update. When that happens
     * EncryptedSharedPreferences throws on OPEN, not on read, so without this
     * the app cannot open its store at all and every call fails forever. There
     * is no way to recover the old bytes; the only choice is between starting
     * clean and being permanently broken. Starting clean costs one sign-in.
     */
    // synchronized because Capacitor dispatches plugin methods on a thread
    // pool: loadToken() during boot, a geofence flush, and an in-flight
    // request can all be calling in at once. Two threads entering the recovery
    // path together would have one deleting the store file while the other
    // held a live handle to it.
    private synchronized SharedPreferences store() throws GeneralSecurityException, IOException {
        if (prefs != null) return prefs;
        Context ctx = getContext();
        try {
            prefs = open(ctx);
        } catch (GeneralSecurityException | IOException e) {
            deleteStoreFile(ctx);
            // A second failure is a real fault and is allowed to propagate --
            // retrying forever would turn a broken device into a silent
            // signed-out loop with nothing in the log to explain it.
            prefs = open(ctx);
        }
        return prefs;
    }

    private SharedPreferences open(Context ctx) throws GeneralSecurityException, IOException {
        // MasterKeys, not MasterKey.Builder: the builder API only exists from
        // security-crypto 1.1.0-alpha onwards, and this pins the 1.0.0 stable
        // release on purpose (see variables.gradle). MasterKeys is deprecated
        // in that later line, which is why the two are easy to confuse -- the
        // deprecation does not apply to the version actually in use here.
        //
        // Note the argument order differs from the 1.1.0 overload too: file
        // name first, context third.
        String masterKeyAlias = MasterKeys.getOrCreate(MasterKeys.AES256_GCM_SPEC);
        return EncryptedSharedPreferences.create(
            FILE_NAME,
            masterKeyAlias,
            ctx,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
        );
    }

    private void deleteStoreFile(Context ctx) {
        // deleteSharedPreferences() is API 24+, and minSdk here is 23, so the
        // file is removed by hand on anything older.
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.N) {
            ctx.deleteSharedPreferences(FILE_NAME);
            return;
        }
        // The in-memory copy has to go too, or the deleted file is written
        // straight back from it the next time anything commits.
        ctx.getSharedPreferences(FILE_NAME, Context.MODE_PRIVATE).edit().clear().commit();
        File f = new File(ctx.getApplicationInfo().dataDir + "/shared_prefs/" + FILE_NAME + ".xml");
        if (!f.delete()) {
            // Nothing to do about it here, and it is not necessarily wrong --
            // the file may simply not exist yet. open() below reports the real
            // failure if there is one.
            android.util.Log.w("SecureStore", "could not delete " + f.getName());
        }
    }

    @PluginMethod
    public void get(PluginCall call) {
        String key = call.getString("key");
        if (key == null) {
            call.reject("key is required");
            return;
        }
        try {
            JSObject ret = new JSObject();
            // Absent reads back as null rather than as an error: "no session"
            // is an ordinary answer, not a failure, and native/auth.ts treats
            // it as signed out.
            ret.put("value", store().getString(key, null));
            call.resolve(ret);
        } catch (Exception e) {
            call.reject("could not read secure storage", e);
        }
    }

    @PluginMethod
    public void set(PluginCall call) {
        String key = call.getString("key");
        String value = call.getString("value");
        if (key == null || value == null) {
            call.reject("key and value are required");
            return;
        }
        try {
            // commit(), not apply(): the caller awaits this before treating the
            // session as persisted, and apply() would resolve the promise while
            // the write was still in flight -- a process killed in that window
            // loses a token the app has already reported as saved.
            if (!store().edit().putString(key, value).commit()) {
                call.reject("could not write secure storage");
                return;
            }
            call.resolve();
        } catch (Exception e) {
            call.reject("could not write secure storage", e);
        }
    }

    @PluginMethod
    public void remove(PluginCall call) {
        String key = call.getString("key");
        if (key == null) {
            call.reject("key is required");
            return;
        }
        try {
            store().edit().remove(key).commit();
            call.resolve();
        } catch (Exception e) {
            call.reject("could not clear secure storage", e);
        }
    }
}
