package ai.agentmesh.app;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.Mockito.doThrow;
import static org.mockito.Mockito.mock;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

import android.content.Context;

import androidx.test.core.app.ApplicationProvider;

import com.google.android.gms.location.GeofencingClient;
import com.google.android.gms.location.GeofencingRequest;
import com.google.android.gms.tasks.OnFailureListener;
import com.google.android.gms.tasks.OnSuccessListener;
import com.google.android.gms.tasks.Task;

import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.mockito.ArgumentCaptor;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;

/**
 * register() is shared by GeofencePlugin's live call and BootReceiver's
 * reboot replay specifically so the two build a request the same way. A
 * regression here is invisible in either caller's own tests -- it only shows
 * up as one of the two silently building a different request than the
 * other, which is exactly what this class exists to prevent.
 */
@RunWith(RobolectricTestRunner.class)
@Config(sdk = 34)
public class GeofenceRegistrarTest {

    private Context context;
    private GeofencingClient client;
    @SuppressWarnings("unchecked")
    private final Task<Void> task = mock(Task.class);

    private Boolean resultSuccess;
    private String resultError;

    private final GeofenceRegistrar.Callback callback = (success, error) -> {
        resultSuccess = success;
        resultError = error;
    };

    @Before
    public void setUp() {
        context = ApplicationProvider.getApplicationContext();
        client = mock(GeofencingClient.class);
        when(task.addOnSuccessListener(any())).thenReturn(task);
        when(task.addOnFailureListener(any())).thenReturn(task);
        when(client.addGeofences(any(GeofencingRequest.class), any())).thenReturn(task);
    }

    @Test
    public void register_onSuccess_reportsSuccessWithNoError() {
        GeofenceRegistrar.register(context, client, "workflow-1", 12.5, 77.5, 150.0, callback);

        ArgumentCaptor<OnSuccessListener<Void>> captor = ArgumentCaptor.forClass(OnSuccessListener.class);
        verify(task).addOnSuccessListener(captor.capture());
        captor.getValue().onSuccess(null);

        assertTrue(resultSuccess);
        assertEquals(null, resultError);
    }

    @Test
    public void register_onGmsFailure_reportsFailureWithTheGmsMessage() {
        GeofenceRegistrar.register(context, client, "workflow-1", 12.5, 77.5, 150.0, callback);

        ArgumentCaptor<OnFailureListener> captor = ArgumentCaptor.forClass(OnFailureListener.class);
        verify(task).addOnFailureListener(captor.capture());
        captor.getValue().onFailure(new RuntimeException("play services unavailable"));

        assertFalse(resultSuccess);
        assertEquals("play services unavailable", resultError);
    }

    // Tagged distinctly per GeofenceRegistrar's own comment: a permission
    // revoked between the caller's check and this call must be
    // distinguishable from an ordinary GMS/network failure, because it is a
    // different thing to tell the user and to triage.
    @Test
    public void register_permissionRevoked_reportsItAsSuchRatherThanAGenericFailure() {
        doThrow(new SecurityException("no ACCESS_BACKGROUND_LOCATION"))
                .when(client).addGeofences(any(GeofencingRequest.class), any());

        GeofenceRegistrar.register(context, client, "workflow-1", 12.5, 77.5, 150.0, callback);

        assertFalse(resultSuccess);
        assertTrue(resultError.startsWith("permission revoked"));
    }

    // A corrupted GeofenceStore entry (radius <= 0 surviving in
    // SharedPreferences) must resolve the callback rather than throw --
    // BootReceiver calls register() once per persisted fence, in a loop,
    // with no try/catch of its own. An uncaught exception here would skip
    // finish() for this fence and leak the whole batch's PendingResult,
    // silently killing reboot recovery for every OTHER workflow queued
    // behind it too.
    @Test
    public void register_invalidRadius_reportsFailureInsteadOfThrowing() {
        GeofenceRegistrar.register(context, client, "workflow-1", 12.5, 77.5, -1.0, callback);

        assertFalse(resultSuccess);
        assertTrue(resultError.startsWith("invalid geofence parameters"));
    }
}
