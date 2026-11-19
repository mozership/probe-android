import java.io.File;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.util.concurrent.TimeoutException;

/**
 * Writes the screen to a file as uiautomator dump does, but reads a screen
 * that does not go idle, as one with a clock or a live value, rather than
 * fail with "could not get idle state". It waits for the screen to go idle
 * for a while, as after a tap, and then reads it as it is.
 *
 * <p>It runs with app_process, beside the uiautomator.jar of the device,
 * whose classes it calls, as the dump command of uiautomator does:
 *
 * <pre>
 * CLASSPATH=/system/framework/uiautomator.jar:dumper.jar \
 *   app_process /system/bin ProbeAndroidDump /data/local/tmp/screen.xml
 * </pre>
 *
 * <p>Those classes, and the ones of Android, are reached by reflection, so
 * that the class compiles with a JDK alone.
 */
public final class ProbeAndroidDump {
    /** How long the screen must keep still to be idle, as for uiautomator. */
    private static final long IDLE_MS = 1000;

    /** How long to wait for the screen to go idle before reading it. */
    private static final long WAIT_MS = 3000;

    public static void main(String[] args) throws Throwable {
        if (args.length != 1) {
            System.err.println("usage: ProbeAndroidDump <file>");
            System.exit(2);
        }
        try {
            dump(new File(args[0]));
        } catch (InvocationTargetException e) {
            throw e.getCause();
        }
    }

    private static void dump(File file) throws Exception {
        Class<?> wrapperClass = Class.forName("com.android.uiautomator.core.UiAutomationShellWrapper");
        Object wrapper = wrapperClass.getConstructor().newInstance();
        wrapperClass.getMethod("connect").invoke(wrapper);
        try {
            // Not compressed, as uiautomator dump without --compressed.
            wrapperClass.getMethod("setCompressedLayoutHierarchy", boolean.class).invoke(wrapper, false);
            Object automation = wrapperClass.getMethod("getUiAutomation").invoke(wrapper);
            Class<?> automationClass = Class.forName("android.app.UiAutomation");
            try {
                automationClass.getMethod("waitForIdle", long.class, long.class).invoke(automation, IDLE_MS, WAIT_MS);
            } catch (InvocationTargetException e) {
                if (!(e.getCause() instanceof TimeoutException)) {
                    throw e;
                }
            }
            Method rootOf = automationClass.getMethod("getRootInActiveWindow");
            Object root = rootOf.invoke(automation);
            for (int i = 0; root == null && i < 10; i++) {
                Thread.sleep(100);
                root = rootOf.invoke(automation);
            }
            if (root == null) {
                System.err.println("ERROR: no window to read.");
                System.exit(1);
            }

            Class<?> displays = Class.forName("android.hardware.display.DisplayManagerGlobal");
            Object global = displays.getMethod("getInstance").invoke(null);
            Object display = displays.getMethod("getRealDisplay", int.class).invoke(global, 0);
            Class<?> displayClass = Class.forName("android.view.Display");
            int rotation = (Integer) displayClass.getMethod("getRotation").invoke(display);
            Class<?> pointClass = Class.forName("android.graphics.Point");
            Object size = pointClass.getConstructor().newInstance();
            displayClass.getMethod("getSize", pointClass).invoke(display, size);
            int width = pointClass.getField("x").getInt(size);
            int height = pointClass.getField("y").getInt(size);

            Class<?> dumper = Class.forName("com.android.uiautomator.core.AccessibilityNodeInfoDumper");
            Class<?> nodeClass = Class.forName("android.view.accessibility.AccessibilityNodeInfo");
            dumper.getMethod("dumpWindowToFile", nodeClass, File.class, int.class, int.class, int.class)
                    .invoke(null, root, file, rotation, width, height);
        } finally {
            wrapperClass.getMethod("disconnect").invoke(wrapper);
        }
    }
}
