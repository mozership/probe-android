// Command probe-android serves a Probe action that drives an app on an
// Android emulator or device through adb. Probe starts it for a step that
// uses github.com/mozership/probe-android@<commit>.
package main

import "github.com/mozership/probe-android/android"

// version is set at build time.
var version = "dev" //nolint:unused // set by -X main.version in .goreleaser

func main() {
	android.Serve()
}
