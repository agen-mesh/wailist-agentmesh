package ai.agentmesh.app;

import android.content.pm.ApplicationInfo;
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
    }
}
