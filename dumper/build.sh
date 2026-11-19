#!/bin/sh
# Builds internal/adb/dumper.jar, the dex of ProbeAndroidDump.java, which
# the action pushes to the device to read a screen that does not go idle.
# It runs javac and d8 in a container, so that it needs Docker alone, and
# the same source builds the same jar. Run it from the repository root:
#
#   dumper/build.sh
#
# CI runs it and fails when the jar committed is not the one it builds.
set -eu

# The JDK, and d8 of R8, as Google's Maven repository serves it.
jdk=eclipse-temurin:21-jdk@sha256:db5cc6341a1d7bc40cdb51e1f0d9824f3bb2cc8107d29da421dc4df7d11f05f1
r8_version=8.13.25
r8_sha256=0d4f2f68c4fcbc9434eff122f41feb6fb1d98efd2faa530cec1abacab1cea03d

root=$(cd "$(dirname "$0")/.." && pwd)
docker run --rm -v "$root:/src" -w /src "$jdk" sh -eu -c "
work=\$(mktemp -d)
curl -fsSL -o \"\$work/r8.jar\" https://dl.google.com/dl/android/maven2/com/android/tools/r8/$r8_version/r8-$r8_version.jar
echo \"$r8_sha256  \$work/r8.jar\" | sha256sum -c - >/dev/null
javac --release 8 -nowarn -d \"\$work/classes\" dumper/ProbeAndroidDump.java
java -cp \"\$work/r8.jar\" com.android.tools.r8.D8 --release --min-api 21 --output \"\$work/dumper.jar\" \"\$work/classes/ProbeAndroidDump.class\"
cp \"\$work/dumper.jar\" internal/adb/dumper.jar
chown $(id -u):$(id -g) internal/adb/dumper.jar
"
