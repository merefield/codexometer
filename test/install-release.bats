#!/usr/bin/env bats

setup() {
  export TEST_ROOT
  TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/codexometer-release-install-test.XXXXXX")"
  export FIXTURE_DIR="$TEST_ROOT/fixture"
  export FIXTURE_TAG=v1.2.3
  export FIXTURE_ASSET=codexometer_1.2.3_linux_amd64.tar.gz
  export CURL_LOG="$TEST_ROOT/curl.log"
  export FAKE_UNAME_S=Linux
  export FAKE_UNAME_M=x86_64
  mkdir -p "$TEST_ROOT/fakebin" "$TEST_ROOT/bin" "$FIXTURE_DIR/package"

  cat > "$FIXTURE_DIR/package/codexometer" <<'EOF'
#!/bin/sh
echo "codexometer 1.2.3"
EOF
  chmod +x "$FIXTURE_DIR/package/codexometer"
  tar -czf "$FIXTURE_DIR/archive.tar.gz" -C "$FIXTURE_DIR/package" codexometer
  export FIXTURE_HASH
  FIXTURE_HASH=$(sha256sum "$FIXTURE_DIR/archive.tar.gz" | awk '{ print $1 }')

  cat > "$TEST_ROOT/fakebin/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) printf '%s\n' "$FAKE_UNAME_S" ;;
  -m) printf '%s\n' "$FAKE_UNAME_M" ;;
  *) exit 2 ;;
esac
EOF
  chmod +x "$TEST_ROOT/fakebin/uname"

  cat > "$TEST_ROOT/fakebin/curl" <<'EOF'
#!/bin/sh
output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output)
      output=$2
      shift 2
      ;;
    --retry)
      shift 2
      ;;
    --*)
      shift
      ;;
    *)
      url=$1
      shift
      ;;
  esac
done
[ -n "$output" ] && [ -n "$url" ] || exit 2
printf '%s\n' "$url" >> "$CURL_LOG"
case "$url" in
  */repos/*/releases/latest)
    printf '{"tag_name":"%s"}\n' "$FIXTURE_TAG" > "$output"
    ;;
  */checksums.txt)
    printf '%s  %s\n' "$FIXTURE_HASH" "$FIXTURE_ASSET" > "$output"
    ;;
  */"$FIXTURE_ASSET")
    cp "$FIXTURE_DIR/archive.tar.gz" "$output"
    ;;
  *)
    printf 'unexpected URL: %s\n' "$url" >&2
    exit 3
    ;;
esac
EOF
  chmod +x "$TEST_ROOT/fakebin/curl"
}

teardown() {
  rm -rf "$TEST_ROOT"
}

@test "release installer resolves, verifies, and installs the latest release" {
  run env \
    PATH="$TEST_ROOT/fakebin:$PATH" \
    CODEXOMETER_BIN_DIR="$TEST_ROOT/bin" \
    sh ./install-release.sh

  [ "$status" -eq 0 ]
  [ -x "$TEST_ROOT/bin/codexometer" ]
  [[ "$output" == *"Verified the release checksum."* ]]
  [[ "$output" == *"Installed codexometer to $TEST_ROOT/bin/codexometer (codexometer 1.2.3)."* ]]
  grep -q '/repos/merefield/codexometer/releases/latest$' "$CURL_LOG"
  grep -q '/releases/download/v1.2.3/codexometer_1.2.3_linux_amd64.tar.gz$' "$CURL_LOG"

  run "$TEST_ROOT/bin/codexometer" --version
  [ "$status" -eq 0 ]
  [ "$output" = "codexometer 1.2.3" ]
}

@test "release installer supports an explicit version and Darwin ARM64" {
  export FAKE_UNAME_S=Darwin
  export FAKE_UNAME_M=arm64
  export FIXTURE_ASSET=codexometer_1.2.3_darwin_arm64.tar.gz

  run env \
    PATH="$TEST_ROOT/fakebin:$PATH" \
    CODEXOMETER_BIN_DIR="$TEST_ROOT/bin" \
    sh ./install-release.sh --version v1.2.3

  [ "$status" -eq 0 ]
  [ -x "$TEST_ROOT/bin/codexometer" ]
  ! grep -q '/releases/latest$' "$CURL_LOG"
  grep -q '/releases/download/v1.2.3/codexometer_1.2.3_darwin_arm64.tar.gz$' "$CURL_LOG"
}

@test "release installer refuses a checksum mismatch" {
  export FIXTURE_HASH=0000000000000000000000000000000000000000000000000000000000000000

  run env \
    PATH="$TEST_ROOT/fakebin:$PATH" \
    CODEXOMETER_BIN_DIR="$TEST_ROOT/bin" \
    sh ./install-release.sh

  [ "$status" -eq 1 ]
  [[ "$output" == *"SHA-256 checksum verification failed"* ]]
  [ ! -e "$TEST_ROOT/bin/codexometer" ]
}

@test "release installer rejects unsupported platforms before downloading" {
  export FAKE_UNAME_S=FreeBSD

  run env \
    PATH="$TEST_ROOT/fakebin:$PATH" \
    CODEXOMETER_BIN_DIR="$TEST_ROOT/bin" \
    sh ./install-release.sh

  [ "$status" -eq 1 ]
  [[ "$output" == *"unsupported operating system: FreeBSD"* ]]
  [ ! -e "$CURL_LOG" ]
}

@test "release installer rejects a bin path that is not a directory" {
  occupied_path="$TEST_ROOT/not-a-directory"
  printf 'occupied\n' > "$occupied_path"

  run env \
    PATH="$TEST_ROOT/fakebin:$PATH" \
    CODEXOMETER_BIN_DIR="$occupied_path" \
    sh ./install-release.sh

  [ "$status" -eq 1 ]
  [[ "$output" == *"CODEXOMETER_BIN_DIR exists and is not a directory: $occupied_path"* ]]
  [ ! -e "$CURL_LOG" ]

  symlink_path="$TEST_ROOT/file-symlink"
  ln -s "$occupied_path" "$symlink_path"

  run env \
    PATH="$TEST_ROOT/fakebin:$PATH" \
    CODEXOMETER_BIN_DIR="$symlink_path" \
    sh ./install-release.sh

  [ "$status" -eq 1 ]
  [[ "$output" == *"CODEXOMETER_BIN_DIR exists and is not a directory: $symlink_path"* ]]
  [ ! -e "$CURL_LOG" ]
}

@test "release installer rejects a binary with the wrong version" {
  cat > "$FIXTURE_DIR/package/codexometer" <<'EOF'
#!/bin/sh
echo "codexometer 9.9.9"
EOF
  chmod +x "$FIXTURE_DIR/package/codexometer"
  tar -czf "$FIXTURE_DIR/archive.tar.gz" -C "$FIXTURE_DIR/package" codexometer
  export FIXTURE_HASH
  FIXTURE_HASH=$(sha256sum "$FIXTURE_DIR/archive.tar.gz" | awk '{ print $1 }')

  run env \
    PATH="$TEST_ROOT/fakebin:$PATH" \
    CODEXOMETER_BIN_DIR="$TEST_ROOT/bin" \
    sh ./install-release.sh

  [ "$status" -eq 1 ]
  [[ "$output" == *"downloaded binary reported an unexpected version: codexometer 9.9.9"* ]]
  [ ! -e "$TEST_ROOT/bin/codexometer" ]
}
