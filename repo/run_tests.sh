#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TOTAL=0
PASSED=0
FAILED=0

run_suite() {
  local name="$1"
  local script_path="$2"
  local out_file
  out_file="$(mktemp)"

  echo "=== Running ${name} ==="
  bash "${script_path}" >"${out_file}" 2>&1
  local suite_exit=$?
  cat "${out_file}"

  local stotal spassed sfailed
  stotal="$(grep -E '^TOTAL=' "${out_file}" | tail -n 1 | cut -d'=' -f2)"
  spassed="$(grep -E '^PASSED=' "${out_file}" | tail -n 1 | cut -d'=' -f2)"
  sfailed="$(grep -E '^FAILED=' "${out_file}" | tail -n 1 | cut -d'=' -f2)"

  if [[ -z "${stotal}" || -z "${spassed}" || -z "${sfailed}" ]]; then
    echo "TEST=${name}.summary_parse"
    echo "STATUS=FAIL"
    echo "REASON=unable to parse suite summary"
    echo "LOG_SNIPPET=expected TOTAL=/PASSED=/FAILED= markers"
    echo "---"
    stotal=1
    spassed=0
    sfailed=1
  fi

  TOTAL=$((TOTAL + stotal))
  PASSED=$((PASSED + spassed))
  FAILED=$((FAILED + sfailed))

  if [[ ${suite_exit} -ne 0 ]]; then
    echo "SUITE_STATUS ${name}=FAIL"
  else
    echo "SUITE_STATUS ${name}=PASS"
  fi
  echo

  rm -f "${out_file}"
}

run_suite "unit_tests" "${ROOT_DIR}/unit_tests/run_unit_tests.sh"
run_suite "API_tests" "${ROOT_DIR}/API_tests/run_api_tests.sh"

echo "=== Final Summary ==="
echo "TOTAL=${TOTAL}"
echo "PASSED=${PASSED}"
echo "FAILED=${FAILED}"

if [[ ${FAILED} -gt 0 ]]; then
  exit 1
fi
exit 0
