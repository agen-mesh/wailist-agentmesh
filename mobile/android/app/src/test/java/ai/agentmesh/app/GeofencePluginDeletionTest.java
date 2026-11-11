package ai.agentmesh.app;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;
import static org.robolectric.Shadows.shadowOf;

import android.Manifest;
import android.app.Application;
import android.app.PendingIntent;
import android.content.Context;
import android.content.SharedPreferences;
import android.location.Location;
import android.os.Looper;

import androidx.test.core.app.ApplicationProvider;

import com.getcapacitor.PluginCall;
import com.google.android.gms.location.GeofencingClient;
import com.google.android.gms.location.GeofencingRequest;
import com.google.android.gms.tasks.CancellationTokenSource;
import com.google.android.gms.tasks.TaskCompletionSource;
import com.google.android.gms.tasks.Tasks;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Set;

@RunWith(RobolectricTestRunner.class)
@Config(sdk = 34)
public class GeofencePluginDeletionTest {
    private Context context;
    private GeofencePlugin plugin;
    private GeofencingClient client;
    private SharedPreferences queue;
    private final List<Registration> registrations = new ArrayList<>();
    private final Set<String> osFences = new HashSet<>();
    private boolean removalFails;

    private static final class Registration {
        final String id;
        final CancellationTokenSource cancellation = new CancellationTokenSource();
        final TaskCompletionSource<Void> completion = new TaskCompletionSource<>(cancellation.getToken());

        Registration(String id) { this.id = id; }
    }

    @Before
    public void setUp() throws Exception {
        context = ApplicationProvider.getApplicationContext();
        shadowOf((Application) context).grantPermissions(
                Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_BACKGROUND_LOCATION);
        client = mock(GeofencingClient.class);
        plugin = new GeofencePlugin() {
            @Override public Context getContext() { return context; }
        };
        Field clientField = GeofencePlugin.class.getDeclaredField("client");
        clientField.setAccessible(true);
        clientField.set(plugin, client);
        queue = context.getSharedPreferences(GeofenceReceiver.PREFS, Context.MODE_PRIVATE);
        when(client.addGeofences(any(GeofencingRequest.class), any())).thenAnswer(invocation -> {
            GeofencingRequest request = invocation.getArgument(0);
            Registration registration = new Registration(request.getGeofences().get(0).getRequestId());
            registrations.add(registration);
            return registration.completion.getTask();
        });
        when(client.removeGeofences(any(PendingIntent.class))).thenAnswer(invocation -> {
            if (removalFails) return Tasks.forException(new SecurityException("permission revoked"));
            osFences.clear();
            return Tasks.forResult(null);
        });
    }

    private PluginCall add(String id, double lat) {
        PluginCall call = mock(PluginCall.class);
        when(call.getString("id")).thenReturn(id);
        when(call.getDouble("lat")).thenReturn(lat);
        when(call.getDouble("lng")).thenReturn(2.0);
        when(call.getDouble("radiusM")).thenReturn(200.0);
        plugin.addGeofence(call);
        return call;
    }

    private void succeed(int index) {
        Registration registration = registrations.get(index);
        osFences.add(registration.id);
        registration.completion.setResult(null);
        shadowOf(Looper.getMainLooper()).idle();
    }

    private void append(String id) throws Exception {
        Method method = GeofenceReceiver.class.getDeclaredMethod("append", Context.class, String.class, Location.class);
        method.setAccessible(true);
        Location location = new Location("test");
        location.setLatitude(1.0);
        location.setLongitude(2.0);
        method.invoke(new GeofenceReceiver(), context, id, location);
    }

    private void assertEmpty() {
        assertTrue(GeofenceStore.loadAll(context).isEmpty());
        assertFalse(queue.contains(GeofenceReceiver.QUEUE_KEY));
        assertFalse(queue.contains("agentmesh.geofence.queue"));
        assertTrue(osFences.isEmpty());
    }

    @Test
    public void completedRegistration_canPersistAndQueue_beforeDeletionClearsIt() throws Exception {
        PluginCall registration = add("owned", 1.0);
        succeed(0);
        verify(registration).resolve();
        assertEquals(1, GeofenceStore.loadAll(context).size());
        append("owned");
        assertTrue(queue.contains(GeofenceReceiver.QUEUE_KEY));
        PluginCall clearing = mock(PluginCall.class);
        plugin.clearAccountData(clearing);
        shadowOf(Looper.getMainLooper()).idle();
        verify(clearing).resolve();
        assertEmpty();
    }

    @Test
    public void delayedSuccess_cannotPersistOrQueueDeletedCoordinates() throws Exception {
        PluginCall registration = add("owned", 1.0);
        queue.edit().putString("agentmesh.geofence.queue", "old location").commit();
        PluginCall clearing = mock(PluginCall.class);
        plugin.clearAccountData(clearing);
        shadowOf(Looper.getMainLooper()).idle();
        assertTrue(GeofenceStore.loadAll(context).isEmpty());
        succeed(0);
        append("owned");
        assertEmpty();
        verify(registration, never()).resolve();
        verify(clearing).resolve();
    }

    @Test
    public void newAccountRegistration_waitsForOldCallbackAndClear_beforeReusingAnId() {
        add("same-id", 1.0);
        PluginCall clearing = mock(PluginCall.class);
        plugin.clearAccountData(clearing);
        PluginCall nextAccount = add("same-id", 55.0);
        shadowOf(Looper.getMainLooper()).idle();
        assertEquals(1, registrations.size());
        succeed(0);
        verify(clearing).resolve();
        assertTrue(GeofenceStore.loadAll(context).isEmpty());
        assertEquals(2, registrations.size());
        succeed(1);
        verify(nextAccount).resolve();
        assertEquals(1, GeofenceStore.loadAll(context).size());
        assertEquals(55.0, GeofenceStore.loadAll(context).get(0).lat, 0.0);
        assertTrue(osFences.contains("same-id"));
    }

    @Test
    public void registrationQueuedBeforeClear_neverReachesTheOperatingSystem() {
        add("first", 1.0);
        PluginCall queued = add("second", 2.0);
        PluginCall clearing = mock(PluginCall.class);
        plugin.clearAccountData(clearing);
        assertEquals(1, registrations.size());
        succeed(0);
        verify(queued).reject(any(String.class));
        verify(clearing).resolve();
        assertEquals(1, registrations.size());
        assertEmpty();
    }

    @Test
    public void bootSnapshotReadBeforeClear_cannotRestoreTheDeletedFence() {
        long generation;
        GeofenceStore.Active snapshot;
        synchronized (GeofenceReceiver.LOCK) {
            GeofenceStore.save(context, "old-boot-fence", 1.0, 2.0, 200.0);
            generation = GeofenceRegistrar.generation();
            snapshot = GeofenceStore.loadAll(context).get(0);
        }
        PluginCall clearing = mock(PluginCall.class);
        plugin.clearAccountData(clearing);
        shadowOf(Looper.getMainLooper()).idle();
        verify(clearing).resolve();
        List<Boolean> results = new ArrayList<>();
        GeofenceRegistrar.register(context, client, generation, snapshot.id, snapshot.lat, snapshot.lng,
                snapshot.radiusM, (success, error) -> results.add(success));
        assertEquals(List.of(false), results);
        assertTrue(registrations.isEmpty());
        assertEmpty();
    }

    @Test
    public void delayedRemoval_cannotRemoveTheNextAccountsSavedFence() {
        add("same-id", 1.0);
        succeed(0);
        TaskCompletionSource<Void> removal = new TaskCompletionSource<>();
        when(client.removeGeofences(any(List.class))).thenReturn(removal.getTask());
        PluginCall removing = mock(PluginCall.class);
        when(removing.getString("id")).thenReturn("same-id");
        plugin.removeGeofence(removing);
        PluginCall clearing = mock(PluginCall.class);
        plugin.clearAccountData(clearing);
        PluginCall nextAccount = add("same-id", 55.0);
        assertEquals(1, registrations.size());
        removal.setResult(null);
        shadowOf(Looper.getMainLooper()).idle();
        verify(removing).reject(any(String.class));
        verify(clearing).resolve();
        succeed(1);
        verify(nextAccount).resolve();
        assertEquals(55.0, GeofenceStore.loadAll(context).get(0).lat, 0.0);
    }

    private void unsuccessfulRegistrationDoesNotBlockCleanup(boolean cancelled) {
        PluginCall registration = add("owned", 1.0);
        PluginCall clearing = mock(PluginCall.class);
        plugin.clearAccountData(clearing);
        if (cancelled) registrations.get(0).cancellation.cancel();
        else registrations.get(0).completion.setException(new IllegalStateException("services unavailable"));
        shadowOf(Looper.getMainLooper()).idle();
        verify(registration).reject(any(String.class));
        verify(clearing).resolve();
        assertEmpty();
        PluginCall nextAccount = add("new-account", 55.0);
        succeed(1);
        verify(nextAccount).resolve();
        assertEquals("new-account", GeofenceStore.loadAll(context).get(0).id);
    }

    @Test
    public void cancelledRegistration_doesNotBlockCleanupOrTheNextAccount() {
        unsuccessfulRegistrationDoesNotBlockCleanup(true);
    }

    @Test
    public void failedRegistration_doesNotBlockCleanupOrTheNextAccount() {
        unsuccessfulRegistrationDoesNotBlockCleanup(false);
    }

    @Test
    public void removalError_keepsLocalDataCleared_andAllowsCleanupRetry() {
        add("owned", 1.0);
        succeed(0);
        removalFails = true;
        PluginCall clearing = mock(PluginCall.class);
        plugin.clearAccountData(clearing);
        shadowOf(Looper.getMainLooper()).idle();
        verify(clearing, never()).resolve();
        assertTrue(GeofenceStore.loadAll(context).isEmpty());
        removalFails = false;
        PluginCall retry = mock(PluginCall.class);
        plugin.clearAccountData(retry);
        shadowOf(Looper.getMainLooper()).idle();
        verify(retry).resolve();
        assertEmpty();
        PluginCall nextAccount = add("new-account", 55.0);
        succeed(1);
        verify(nextAccount).resolve();
    }
}
