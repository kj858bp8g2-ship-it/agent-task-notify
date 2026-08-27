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
cleanup_on_exit() {
    local body_status=$?
    local cleanup_status=0
    cleanup || cleanup_status=$?
    if test "$body_status" -ne 0; then
        exit "$body_status"
    fi
    exit "$cleanup_status"
}
trap cleanup_on_exit EXIT

# /usr/bin/security creates and locks only this test fixture; production code
# uses the native Keychain API and never invokes this command.
"$security_bin" create-keychain -p atn-synthetic-ci-fixture-only "$test_keychain"
"$security_bin" set-keychain-settings -lut 3600 "$test_keychain"
"$security_bin" unlock-keychain -p atn-synthetic-ci-fixture-only "$test_keychain"
"$security_bin" list-keychains -d user | sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//' > "$previous_search"
"$security_bin" default-keychain -d user | sed -e 's/^[[:space:]]*"//' -e 's/"[[:space:]]*$//' > "$previous_default"
if ! test -s "$previous_search" || ! test -s "$previous_default"; then
    echo "native macOS CI fixture requires nonempty Keychain backups" >&2
    exit 1
fi
search_mutated=true
default_mutated=true
"$security_bin" list-keychains -d user -s "$test_keychain"
"$security_bin" default-keychain -d user -s "$test_keychain"

export ATN_TEST_KEYCHAIN="$test_keychain"
export TMPDIR="$test_tmp"
export TMP="$test_tmp"
export TEMP="$test_tmp"
export GOTMPDIR="$test_tmp"

filesec_counterfactual() {
    local counter_root="$test_tmp/filesec-counterfactual"
    local counter_log="$fixture_dir/filesec-counterfactual.log"
    local counter_status=0
    if ! mkdir -p "$counter_root/internal" 2>/dev/null ||
        ! cp go.mod go.sum "$counter_root/" 2>/dev/null ||
        ! cp -R internal/store "$counter_root/internal/" 2>/dev/null ||
        ! git show 0062d3b1ccc08c2b81112d8c843b8800f3af4df2:internal/store/acl_darwin.go > "$counter_root/internal/store/acl_darwin.go" 2>/dev/null; then
        echo "filesec-counterfactual: setup failed" >&2
        return 1
    fi
    (cd "$counter_root" && go test -count=1 -v -timeout=45s -run '^TestDarwinRejectsIncompleteFileSecurity$' ./internal/store) > "$counter_log" 2>&1 || counter_status=$?
    if test "$counter_status" -eq 0; then
        echo "filesec-counterfactual: unexpected pass" >&2
        return 1
    fi
    if ! grep -F -- '--- FAIL: TestDarwinRejectsIncompleteFileSecurity' "$counter_log" >/dev/null ||
        ! grep -F -- 'stage=unfilled result=1' "$counter_log" >/dev/null ||
        ! grep -F -- 'incomplete filesec accepted or complete no-ACL filesec rejected' "$counter_log" >/dev/null; then
        echo "filesec-counterfactual: unexpected failure evidence" >&2
        return 1
    fi
    echo "filesec-counterfactual: expected rejection confirmed"
}

filesec_counterfactual
go test -count=1 -v ./... | tee "$fixture_dir/go-test.log"
grep -F -- '--- PASS: TestDarwinLockedKeychainBackgroundDenial' "$fixture_dir/go-test.log"
grep -F -- '--- PASS: TestDarwinRejectsIncompleteFileSecurity' "$fixture_dir/go-test.log"
