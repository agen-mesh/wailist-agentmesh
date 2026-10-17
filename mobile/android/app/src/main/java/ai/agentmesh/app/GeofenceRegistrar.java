package ai.agentmesh.app;

import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.os.Build;

import com.google.android.gms.location.Geofence;
import com.google.android.gms.location.GeofencingClient;
import com.google.android.gms.location.GeofencingRequest;

import java.util.Collections;
import java.util.ArrayDeque;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * Builds and submits a single-circle GeofencingRequest.
 *
 * Shared by GeofencePlugin (a live call from the WebView) and BootReceiver (no
 * WebView, replaying what GeofenceStore remembered) so a boot-time
 * re-registration is built exactly the same way a fresh one is, rather than a
 * second implementation that can quietly drift from the first.
 */
final class GeofenceRegistrar {

    private static final ArrayDeque<Runnable> operations = new ArrayDeque<>();
    private static boolean operationRunning;
    private static long accountGeneration;

    private GeofenceRegistrar() {
    }

    interface Callback {
        void onDone(boolean success, String error);
    }

    static long generation() {
        synchronized (GeofenceReceiver.LOCK) {
            return accountGeneration;
        }
    }

    // The monitor covers submission and persistence; the queue covers asynchronous OS work.
    private static void enqueue(Runnable operation) {
        operations.addLast(operation);
        if (!operationRunning) runNext();
    }

    private static void runNext() {
        Runnable next = operations.pollFirst();
        operationRunning = next != null;
        if (next != null) next.run();
    }

    private static Callback completing(Callback callback) {
        AtomicBoolean completed = new AtomicBoolean();
        return (success, error) -> {
            synchronized (GeofenceReceiver.LOCK) {
                if (!completed.compareAndSet(false, true)) return;
                try {
                    callback.onDone(success, error);
                } finally {
                    runNext();
                }
            }
        };
    }

    private static Callback forGeneration(long generation, Callback callback) {
        return completing((success, error) -> {
            if (generation != accountGeneration) callback.onDone(false, "account location data was cleared");
            else callback.onDone(success, error);
        });
    }

    static PendingIntent transitionIntent(Context context) {
        Intent intent = new Intent(context, GeofenceReceiver.class);
        // FLAG_MUTABLE is required: the OS writes the transition details into
        // this intent before delivering it. An immutable one arrives empty.
        int flags = PendingIntent.FLAG_UPDATE_CURRENT;
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            flags |= PendingIntent.FLAG_MUTABLE;
        }
        return PendingIntent.getBroadcast(context, 0, intent, flags);
    }

    // Takes the caller's GeofencingClient rather than creating its own so
    // GeofencePlugin's add and remove paths keep operating through one
    // instance. BootReceiver has no persistent instance to share and gets a
    // fresh one from LocationServices instead.
    static void register(Context context, GeofencingClient client, String id, double lat, double lng,
            double radiusM, Callback callback) {
        register(context, client, generation(), id, lat, lng, radiusM, callback);
    }

    static void register(Context context, GeofencingClient client, long generation, String id, double lat,
            double lng, double radiusM, Callback callback) {
        synchronized (GeofenceReceiver.LOCK) {
            enqueue(() -> {
                Callback done = forGeneration(generation, callback);
                if (generation != accountGeneration) {
                    done.onDone(false, "account location data was cleared");
                    return;
                }
                registerNow(context, client, id, lat, lng, radiusM, done);
            });
        }
    }

    static void unregister(GeofencingClient client, long generation, String id, Callback callback) {
        synchronized (GeofenceReceiver.LOCK) {
            enqueue(() -> {
                Callback done = forGeneration(generation, callback);
                if (generation != accountGeneration) {
                    done.onDone(false, "account location data was cleared");
                    return;
                }
                try {
                    client.removeGeofences(Collections.singletonList(id))
                            .addOnSuccessListener(unused -> done.onDone(true, null))
                            .addOnFailureListener(e -> done.onDone(false, e.getMessage()))
                            .addOnCanceledListener(() -> done.onDone(false, "geofence removal cancelled"));
                } catch (SecurityException | IllegalArgumentException e) {
                    done.onDone(false, e.getMessage());
                }
            });
        }
    }

    static void clearAccountData(Context context, GeofencingClient client, Callback callback) {
        synchronized (GeofenceReceiver.LOCK) {
            accountGeneration += 1;
            boolean fencesCleared = GeofenceStore.clear(context);
            boolean queueCleared = context.getSharedPreferences(GeofenceReceiver.PREFS, Context.MODE_PRIVATE)
                    .edit().remove(GeofenceReceiver.QUEUE_KEY).remove("agentmesh.geofence.queue").commit();
            // A pending add must settle before the final removal, or the OS can restore it afterward.
            enqueue(() -> {
                Callback done = completing(callback);
                try {
                    client.removeGeofences(transitionIntent(context))
                            .addOnSuccessListener(unused -> done.onDone(fencesCleared && queueCleared,
                                    fencesCleared && queueCleared ? null : "could not clear stored location data"))
                            .addOnFailureListener(e -> done.onDone(false, "could not remove account geofences: " + e.getMessage()))
                            .addOnCanceledListener(() -> done.onDone(false, "account geofence removal cancelled"));
                } catch (SecurityException | IllegalArgumentException e) {
                    done.onDone(false, "could not remove account geofences: " + e.getMessage());
                }
            });
        }
    }

    private static void registerNow(Context context, GeofencingClient client, String id, double lat, double lng,
            double radiusM, Callback callback) {
        // Builder construction is inside the try, not just addGeofences():
        // BootReceiver calls this once per persisted fence with no try/catch
        // of its own, so a single corrupted entry (an out-of-range radius or
        // coordinate surviving in SharedPreferences) throwing
        // IllegalArgumentException here would otherwise escape uncaught,
        // skip pending.finish() for every fence still queued behind it, and
        // leak that PendingResult -- one bad fence taking down reboot
        // recovery for every other workflow's fence in the same batch.
        try {
            Geofence fence = new Geofence.Builder()
                    .setRequestId(id)
                    .setCircularRegion(lat, lng, (float) radiusM)
                    .setExpirationDuration(Geofence.NEVER_EXPIRE)
                    .setTransitionTypes(Geofence.GEOFENCE_TRANSITION_ENTER | Geofence.GEOFENCE_TRANSITION_EXIT)
                    .build();

            GeofencingRequest request = new GeofencingRequest.Builder()
                    // No initial trigger, same reasoning as GeofencePlugin's live
                    // path: reporting the current state the moment a fence is
                    // (re-)registered would fire an "entry" the user never made.
                    .setInitialTrigger(0)
                    .addGeofences(Collections.singletonList(fence))
                    .build();

            client.addGeofences(request, transitionIntent(context))
                    .addOnSuccessListener(unused -> callback.onDone(true, null))
                    .addOnFailureListener(e -> callback.onDone(false, e.getMessage()))
                    .addOnCanceledListener(() -> callback.onDone(false, "geofence registration cancelled"));
        } catch (SecurityException e) {
            // Tagged distinctly from a GMS/network failure: this one means the
            // permission was revoked between the caller's check and this call,
            // which is a different thing to tell the user and to triage than
            // "the request to Play Services failed".
            callback.onDone(false, "permission revoked: " + e.getMessage());
        } catch (IllegalArgumentException e) {
            callback.onDone(false, "invalid geofence parameters: " + e.getMessage());
        }
    }
}
