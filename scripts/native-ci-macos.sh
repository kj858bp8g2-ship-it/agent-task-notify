#!/usr/bin/env bash
set -euo pipefail

# This script may change only a disposable GitHub-hosted runner Keychain.
if test "${CI:-}" != "true" || test -z "${RUNNER_TEMP:-}"; then
    echo "native macOS CI fixture requires CI=true and RUNNER_TEMP" >&2
    exit 2
fi

security_bin=/usr/bin/security
if test "${ATN_TEST_FAKE_SECURITY:-}" = "1"; then
    test -n "${ATN_TEST_SECURITY_BIN:-}" || exit 2
    security_bin="$ATN_TEST_SECURITY_BIN"
fi

runner_temp=$(cd "$RUNNER_TEMP" && pwd -P)
fixture_dir=$(mktemp -d "$runner_temp/atn-keychain.XXXXXXXX")
fixture_dir=$(cd "$fixture_dir" && pwd -P)
test_keychain="$fixture_dir/synthetic.keychain-db"
test_tmp=$(mktemp -d "$runner_temp/atn-go-tmp.XXXXXXXX")
test_tmp=$(cd "$test_tmp" && pwd -P)
previous_search="$fixture_dir/previous-search-keychains.txt"
previous_default="$fixture_dir/previous-default-keychain.txt"
search_mutated=false
default_mutated=false

cleanup() {
    local cleanup_status=0
    set +e
    if test "$default_mutated" = true && test -s "$previous_default"; then
        "$security_bin" default-keychain -d user -s "$(cat "$previous_default")" || cleanup_status=1
    fi
    if test "$search_mutated" = true && test -s "$previous_search"; then
        local previous=()
        local keychain_path
        while IFS= read -r keychain_path; do
            test -n "$keychain_path" && previous+=("$keychain_path")
        done < "$previous_search"
        test "${#previous[@]}" -gt 0 && "$security_bin" list-keychains -d user -s "${previous[@]}" || cleanup_status=1
    fi
    if test -n "$test_keychain" && test "$(dirname "$test_keychain")" = "$fixture_dir" && test "$(basename "$test_keychain")" = "synthetic.keychain-db"; then
        "$security_bin" unlock-keychain -p atn-synthetic-ci-fixture-only "$test_keychain" || cleanup_status=1
        "$security_bin" delete-keychain "$test_keychain" || cleanup_status=1
    fi
    if test -n "$fixture_dir" && test "$(dirname "$fixture_dir")" = "$runner_temp" && [[ "$(basename "$fixture_dir")" == atn-keychain.* ]]; then
        rm -rf -- "$fixture_dir" || cleanup_status=1
    fi
    if test -n "$test_tmp" && test "$(dirname "$test_tmp")" = "$runner_temp" && [[ "$(basename "$test_tmp")" == atn-go-tmp.* ]]; then
        rm -rf -- "$test_tmp" || cleanup_status=1
    fi
    return "$cleanup_status"
}
trap cleanup EXIT

# /usr/bin/security creates and locks only this test fixture; production code
# uses the native Keychain API and never invokes this command.
"$security_bin" create-keychain -p atn-synthetic-ci-fixture-only "$test_keychain"
"$security_bin" set-keychain-settings -lut 3600 "$test_keychain"
"$security_bin" unlock-keychain -p atn-synthetic-ci-fixture-only "$test_keychain"
"$security_bin" list-keychains -d user | sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//' > "$previous_search"
"$security_bin" default-keychain -d user | sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//' > "$previous_default"
test -s "$previous_search" && test -s "$previous_default"
search_mutated=true
default_mutated=true
"$security_bin" list-keychains -d user -s "$test_keychain"
"$security_bin" default-keychain -d user -s "$test_keychain"

export ATN_TEST_KEYCHAIN="$test_keychain"
export TMPDIR="$test_tmp"
export TMP="$test_tmp"
export TEMP="$test_tmp"
export GOTMPDIR="$test_tmp"

go test -count=1 -v ./... | tee "$fixture_dir/go-test.log"
grep -F -- '--- PASS: TestDarwinLockedKeychainBackgroundDenial' "$fixture_dir/go-test.log"
