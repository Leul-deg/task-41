#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
E2E_DIR="${ROOT_DIR}/frontend/e2e"

TOTAL=0
PASSED=0
FAILED=0

print_result() {
  local name="$1" status="$2" reason="${3:-}" snippet="${4:-}"
  echo "TEST=${name}"
  echo "STATUS=${status}"
  [[ -n "${reason}" ]] && echo "REASON=${reason}"
  [[ -n "${snippet}" ]] && echo "LOG_SNIPPET=${snippet}"
  echo "---"
}

fail_all() {
  local reason="$1"
  TOTAL=1; PASSED=0; FAILED=1
  print_result "e2e.setup" "FAIL" "${reason}" ""
  echo "TOTAL=${TOTAL}"
  echo "PASSED=${PASSED}"
  echo "FAILED=${FAILED}"
  exit 1
}

if ! command -v node >/dev/null 2>&1; then
  fail_all "node not found; skipping e2e tests"
fi
if ! command -v npx >/dev/null 2>&1; then
  fail_all "npx not found; skipping e2e tests"
fi

if [[ ! -d "${E2E_DIR}" ]]; then
  fail_all "e2e directory not found at ${E2E_DIR}"
fi

echo "=== Installing e2e dependencies ==="
(cd "${E2E_DIR}" && npm install --prefer-offline --no-audit 2>&1) || fail_all "npm install failed"

echo "=== Running Playwright e2e tests ==="
json_out="$(mktemp).json"
(cd "${E2E_DIR}" && npx playwright test --reporter=json 2>/dev/null) >"${json_out}" 2>&1
playwright_exit=$?

if command -v python3 >/dev/null 2>&1 && [[ -s "${json_out}" ]]; then
  result="$(python3 - "${json_out}" <<'PYEOF'
import json, sys
try:
    with open(sys.argv[1]) as f:
        data = json.load(f)
    stats = data.get("stats", {})
    total = stats.get("expected", 0) + stats.get("unexpected", 0) + stats.get("flaky", 0) + stats.get("skipped", 0)
    passed = stats.get("expected", 0)
    failed = stats.get("unexpected", 0)
    if total == 0:
        suites = data.get("suites", [])
        specs = []
        def collect(s):
            specs.extend(s.get("specs", []))
            for c in s.get("suites", []):
                collect(c)
        for s in suites:
            collect(s)
        total = len(specs)
        passed = sum(1 for s in specs if all(r.get("status") == "passed" for r in s.get("tests", [{}])[0].get("results", [{}])))
        failed = total - passed
    print(f"{total}:{passed}:{failed}")
except Exception as e:
    print(f"0:0:0")
PYEOF
)"
  IFS=':' read -r TOTAL PASSED FAILED <<<"${result}"
fi

if [[ "${TOTAL}" -eq 0 ]]; then
  if [[ ${playwright_exit} -eq 0 ]]; then
    TOTAL=1; PASSED=1; FAILED=0
    print_result "e2e.playwright" "PASS" "" ""
  else
    TOTAL=1; PASSED=0; FAILED=1
    snippet="$(tail -n 5 "${json_out}" 2>/dev/null | tr '\n' '|' | cut -c1-400)"
    print_result "e2e.playwright" "FAIL" "playwright exited ${playwright_exit}" "${snippet}"
  fi
else
  if [[ ${playwright_exit} -eq 0 ]]; then
    print_result "e2e.playwright" "PASS" "" ""
  else
    print_result "e2e.playwright" "FAIL" "playwright exited ${playwright_exit}" ""
  fi
fi

rm -f "${json_out}"

echo "TOTAL=${TOTAL}"
echo "PASSED=${PASSED}"
echo "FAILED=${FAILED}"

[[ ${FAILED} -gt 0 ]] && exit 1
exit 0
