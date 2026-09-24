package ai.agentmesh.app;

import android.content.pm.ApplicationInfo;
import android.content.res.Configuration;
import android.os.Bundle;
import android.webkit.WebView;

import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        // Capacitor auto-discovers plugins shipped as packages; one that lives
        // in the app module has to be registered explicitly, and before
        // super.onCreate, or the bridge is built without it.
        registerPlugin(GeofencePlugin.class);
        registerPlugin(SecureStorePlugin.class);
        registerPlugin(PushAvailabilityPlugin.class);
        super.onCreate(savedInstanceState);

        // Belt and braces on WebView debugging.
        //
        // Capacitor already gets this right on its own: CapConfig defaults
        // android.webContentsDebuggingEnabled to whether the app is debuggable
        // (FLAG_DEBUGGABLE), so a release build is not inspectable and a debug
        // build is. That default is exactly what we want, which is why
        // capacitor.config.ts deliberately does NOT set the key -- setting it
        // false would also kill inspection on debug builds, and setting it true
        // would ship an inspectable release.
        //
        // What the default cannot survive is somebody adding the key later to
        // debug something and not taking it out again. This runs AFTER
        // super.onCreate, which is where the bridge would have enabled it, and
        // turns it back off for any build the platform does not consider
        // debuggable. It costs one branch and removes a whole class of
        // accident: with this here, no configuration change can ship a release
        // whose WebView, network traffic and storage are open to anyone with a
        // USB cable.
        if ((getApplicationInfo().flags & ApplicationInfo.FLAG_DEBUGGABLE) == 0) {
            WebView.setWebContentsDebuggingEnabled(false);
        }

        applyTextZoom(getResources().getConfiguration());
    }

    // fontScale is in configChanges in AndroidManifest.xml, so changing the
    // system font size arrives here instead of recreating the activity. A
    // recreate reloaded the WebView, replayed the launch splash and dropped
    // whatever screen and state the user was on.
    @Override
    public void onConfigurationChanged(Configuration newConfig) {
        super.onConfigurationChanged(newConfig);
        applyTextZoom(newConfig);
    }

    // Follow the phone's text size, all of it.
    //
    // Applied here as well as at startup so a change made while the app is
    // open takes effect without a restart. Deliberately unclamped: the system
    // font size is an accessibility setting, and someone who needs 200% text
    // must get it. Layouts reflow to fit the text, not the other way round.
    private void applyTextZoom(Configuration config) {
        if (getBridge() == null) return;
        getBridge().getWebView().getSettings().setTextZoom(Math.round(config.fontScale * 100));
    }
}
