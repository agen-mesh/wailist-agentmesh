package ai.agentmesh.app;

import android.content.pm.ApplicationInfo;
import android.content.res.Configuration;
import android.os.Bundle;
import android.webkit.WebView;

import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {
    // Bounds for the system font scale the WebView follows; see applyTextZoom.
    private static final float MIN_FONT_SCALE = 0.85f;
    private static final float MAX_FONT_SCALE = 1.15f;

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

    // Follow the phone's text size, within limits.
    //
    // The WebView scales text with Android's system font size, and nothing
    // here bounded it. A larger system font makes text wider while layout
    // widths stay in fixed CSS pixels, so rows sized for the standard scale
    // overflow and get cut off. Clamping keeps a larger system font readable
    // without letting it break layouts built for the standard scale.
    private void applyTextZoom(Configuration config) {
        if (getBridge() == null) return;
        float cappedScale = Math.max(MIN_FONT_SCALE, Math.min(config.fontScale, MAX_FONT_SCALE));
        getBridge().getWebView().getSettings().setTextZoom(Math.round(cappedScale * 100));
    }
}
