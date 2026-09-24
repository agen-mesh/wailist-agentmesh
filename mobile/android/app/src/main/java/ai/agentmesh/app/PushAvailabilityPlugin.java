package ai.agentmesh.app;

import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;

/**
 * Says whether this build can use Firebase Cloud Messaging at all.
 *
 * This exists because asking the push plugin is not safe. Without
 * google-services.json the default FirebaseApp is never created, and the
 * plugin's register() and unregister() both call FirebaseMessaging.getInstance()
 * first. That throws IllegalStateException on the plugin thread, Capacitor's
 * bridge rethrows it as a RuntimeException, and the process dies: tapping
 * "Turn on notifications" closed the app, and so did signing out. No promise
 * rejects, so nothing in JavaScript can catch it.
 *
 * The check reads the google_app_id string resource, which the
 * google-services Gradle plugin generates from google-services.json and which
 * FirebaseInitProvider needs to create the default app. No resource, no app.
 * Reading a resource also keeps Firebase itself off this module's classpath.
 */
@CapacitorPlugin(name = "PushAvailability")
public class PushAvailabilityPlugin extends Plugin {

    @PluginMethod
    public void check(PluginCall call) {
        int id = getContext()
            .getResources()
            .getIdentifier("google_app_id", "string", getContext().getPackageName());
        JSObject result = new JSObject();
        result.put("available", id != 0);
        call.resolve(result);
    }
}
