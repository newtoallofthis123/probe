#!/usr/bin/env bash
set -euo pipefail

PROBE="./probe"
PASS=0
FAIL=0
TOTAL=5

# Check prerequisites
if ! curl -sf http://localhost:11434/api/tags > /dev/null 2>&1; then
    echo "ERROR: Ollama is not reachable at localhost:11434" >&2
    exit 2
fi

if ! command -v jq > /dev/null 2>&1; then
    echo "ERROR: jq is required but not installed" >&2
    exit 2
fi

if [[ ! -x "$PROBE" ]]; then
    echo "ERROR: $PROBE binary not found (run 'just build' first)" >&2
    exit 2
fi

run_test() {
    local name="$1"
    local query="$2"
    local check="$3"  # jq expression that should return "true"

    printf "  %-50s " "$name"

    local output
    local exit_code=0
    output=$($PROBE --json "$query" 2>/dev/null) || exit_code=$?

    # For the zero-result test, accept exit 1 (no results) or exit 0 with empty results
    if [[ "$name" == *"zero-result"* ]]; then
        if [[ $exit_code -eq 1 ]]; then
            echo "PASS"
            PASS=$((PASS + 1))
            return
        fi
        if [[ $exit_code -eq 0 ]] && echo "$output" | jq -e "$check" > /dev/null 2>&1; then
            echo "PASS"
            PASS=$((PASS + 1))
            return
        fi
        echo "FAIL (exit=$exit_code, results=$(echo "$output" | jq -c '.results | length' 2>/dev/null))"
        FAIL=$((FAIL + 1))
        return
    fi

    if [[ $exit_code -ne 0 ]]; then
        echo "FAIL (exit=$exit_code)"
        FAIL=$((FAIL + 1))
        return
    fi

    if echo "$output" | jq -e "$check" > /dev/null 2>&1; then
        echo "PASS"
        PASS=$((PASS + 1))
    else
        echo "FAIL"
        echo "    output: $(echo "$output" | jq -c '.results[:2]' 2>/dev/null || echo "$output" | head -c 200)"
        FAIL=$((FAIL + 1))
    fi
}

echo "=== probe validation tests ==="
echo ""

# Test 1: Basic search finds Go files
run_test "regex-construction: finds .go files" \
    "where is the main function defined?" \
    '[.results[]? | select(.file | endswith(".go"))] | length > 0'

# Test 2: Convergence — agent.go found in ≤5 turns
run_test "convergence: agent loop in agent.go, ≤5 turns" \
    "where is the agent loop implemented?" \
    '([.results[]? | select(.file | endswith("agent.go"))] | length > 0) and .turns <= 5'

# Test 3: Tool selection — all results are test files
run_test "tool-selection: finds only _test.go files" \
    "find all Go test files" \
    '([.results[]?.file | select(endswith("_test.go") | not)] | length == 0) and ([.results[]?.file] | length > 0)'

# Test 4: Zero-result query
run_test "zero-result: quantum flux capacitor" \
    "where is the quantum flux capacitor?" \
    '(.results | length) == 0 or .results == null'

# Test 5: Multi-file results
run_test "multi-file: config loading spans ≥2 files" \
    "how does config loading work?" \
    '[.results[]?.file] | unique | length >= 2'

echo ""
echo "$PASS/$TOTAL tests passed"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
