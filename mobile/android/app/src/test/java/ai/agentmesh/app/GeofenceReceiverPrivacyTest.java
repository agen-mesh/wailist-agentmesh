package ai.agentmesh.app;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertTrue;

import android.content.Context;
import android.content.SharedPreferences;
import android.location.Location;

import androidx.test.core.app.ApplicationProvider;

import org.json.JSONArray;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.robolectric.RobolectricTestRunner;
import org.robolectric.annotation.Config;
import org.robolectric.shadows.ShadowLog;

import java.io.PrintWriter;
import java.io.StringWriter;
import java.lang.reflect.Method;

@RunWith(RobolectricTestRunner.class)
@Config(sdk = 34)
public class GeofenceReceiverPrivacyTest {
    private Context context;
    private SharedPreferences queue;

    @Before
    public void setUp() {
        context = ApplicationProvider.getApplicationContext();
        queue = context.getSharedPreferences(GeofenceReceiver.PREFS, Context.MODE_PRIVATE);
        queue.edit().clear().commit();
        GeofenceStore.save(context, "owned", 51.5007, -0.1246, 100.0);
        ShadowLog.clear();
    }

    private void append() throws Exception {
        Method method = GeofenceReceiver.class.getDeclaredMethod("append", Context.class, String.class, Location.class);
        method.setAccessible(true);
        Location location = new Location("test");
        location.setLatitude(51.500711122);
        location.setLongitude(-0.124622233);
        location.setTime(1_700_000_000_000L);
        method.invoke(new GeofenceReceiver(), context, "owned", location);
    }

    @Test
    public void corruptQueue_recoversWithoutLoggingStoredPositions() throws Exception {
        String raw = "[{\"lat\":12.345678901,\"lng\":-45.678912345},";
        queue.edit().putString(GeofenceReceiver.QUEUE_KEY, raw).commit();
        append();

        JSONArray recovered = new JSONArray(queue.getString(GeofenceReceiver.QUEUE_KEY, "[]"));
        assertEquals(1, recovered.length());
        assertEquals("owned", recovered.getJSONObject(0).getString("workflowId"));
        assertEquals(51.500711122, recovered.getJSONObject(0).getDouble("lat"), 0.0);

        StringWriter logged = new StringWriter();
        PrintWriter writer = new PrintWriter(logged);
        for (ShadowLog.LogItem item : ShadowLog.getLogs()) {
            writer.println(item.msg);
            if (item.throwable != null) item.throwable.printStackTrace(writer);
        }
        writer.flush();
        assertTrue(logged.toString().contains("native queue was corrupt"));
        assertFalse("diagnostic exposed queued latitude", logged.toString().contains("12.345678901"));
        assertFalse("diagnostic exposed queued longitude", logged.toString().contains("45.678912345"));
    }

    @Test
    public void validQueue_preservesPendingCrossings() throws Exception {
        queue.edit().putString(GeofenceReceiver.QUEUE_KEY,
                "[{\"workflowId\":\"owned\",\"lat\":51.5008,\"lng\":-0.1245}]").commit();
        append();
        JSONArray saved = new JSONArray(queue.getString(GeofenceReceiver.QUEUE_KEY, "[]"));
        assertEquals(2, saved.length());
        assertEquals(51.5008, saved.getJSONObject(0).getDouble("lat"), 0.0);
        assertEquals(51.500711122, saved.getJSONObject(1).getDouble("lat"), 0.0);
        assertTrue(ShadowLog.getLogs().isEmpty());
    }
}
