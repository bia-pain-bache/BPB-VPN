#!/usr/bin/env bash
# Fetches Tor Project's official Android expert bundles and installs, per ABI:
#   tor      → android/app/src/main/jniLibs/<abi>/libtor.so
#   lyrebird → android/app/src/main/jniLibs/<abi>/liblyrebird.so   (obfs4, meek, webtunnel, snowflake)
# plus, once: Tor's IPv4 and IPv6 GeoIP files (for ExitNodes by country) and pt_config.json
# (the built-in bridges) as app assets. Both binaries are executables despite the .so names:
# Android only installs native libraries, and runs them from nativeLibraryDir.
# Hashes are from https://dist.torproject.org/torbrowser/$VERSION/sha256sums-signed-build.txt
# Paths are relative to this script (scripts/), so it runs from anywhere.
set -euo pipefail
cd "$(dirname "$0")"

VERSION=15.0.25
BASE="https://dist.torproject.org/torbrowser/$VERSION"
JNI=../android/app/src/main/jniLibs
ASSETS=../android/app/src/main/assets

declare -A ABI=([aarch64]=arm64-v8a [armv7]=armeabi-v7a [x86]=x86 [x86_64]=x86_64)
declare -A SHA=(
  [aarch64]=a1a67ec77ce0a7c4fca446fd9f915fbb0302c3107c2d56ab213196187cfd894a
  [armv7]=4fcdb5540f7ea2f529b9063115d66dd7dfa61e5602bfaab96a78e8046bb4b8d5
  [x86]=a76c881f1b7feb10994cacb4d0bc0b1ba388a25463b4f25985b7805af2091bbb
  [x86_64]=11dbac44e2ee29df2cd6e3a5f38164d2630b2c85342f6a041b9cb57d7012d071
)

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

for arch in aarch64 armv7 x86 x86_64; do
  file="tor-expert-bundle-android-$arch-$VERSION.tar.gz"
  echo "fetching $file"
  curl -fsSL -o "$TMP/$file" "$BASE/$file"
  echo "${SHA[$arch]}  $TMP/$file" | sha256sum -c --quiet -
  mkdir -p "$TMP/$arch" "$JNI/${ABI[$arch]}"
  tar -xzf "$TMP/$file" -C "$TMP/$arch"
  cp "$TMP/$arch/tor/libTor.so" "$JNI/${ABI[$arch]}/libtor.so"
  cp "$TMP/$arch/tor/pluggable_transports/lyrebird" "$JNI/${ABI[$arch]}/liblyrebird.so"
done

cp "$TMP/aarch64/data/geoip" "$ASSETS/tor_geoip"
cp "$TMP/aarch64/data/geoip6" "$ASSETS/tor_geoip6"
cp "$TMP/aarch64/tor/pluggable_transports/pt_config.json" "$ASSETS/tor_pt_config.json"
ls -la "$JNI"/*/libtor.so "$JNI"/*/liblyrebird.so "$ASSETS"/tor_*
