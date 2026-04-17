#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

TOTAL=0
PASSED=0
FAILED=0
STACK_STARTED_BY_SCRIPT=0

have_cmd() {
  command -v "$1" >/dev/null 2>&1
}

have_docker_compose() {
  if ! have_cmd docker; then
    return 1
  fi
  docker compose version >/dev/null 2>&1
}

wait_for_url() {
  local url="$1"
  local attempts="${2:-90}"
  local delay_sec="${3:-1}"
  local i

  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS --max-time 2 "${url}" >/dev/null 2>&1; then
      return 0
    fi
    sleep "${delay_sec}"
  done
  return 1
}

start_stack_for_api_tests_if_needed() {
  if wait_for_url "http://localhost:8081/" 1 1 && wait_for_url "http://localhost:8080/healthz" 1 1; then
    return 0
  fi

  if ! have_docker_compose; then
    echo "ERROR: docker compose unavailable and services are not reachable at http://localhost:8081/ and http://localhost:8080/healthz"
    return 1
  fi

  echo "=== Starting docker compose stack for API tests ==="
  docker compose -f "${ROOT_DIR}/docker-compose.yml" down -v --remove-orphans >/dev/null 2>&1 || true
  if ! docker compose -f "${ROOT_DIR}/docker-compose.yml" up -d --build; then
    echo "ERROR: docker compose up failed"
    return 1
  fi
  STACK_STARTED_BY_SCRIPT=1

  if ! wait_for_url "http://localhost:8081/" 120 1; then
    echo "ERROR: frontend did not become ready at http://localhost:8081/"
    return 1
  fi
  if ! wait_for_url "http://localhost:8080/healthz" 120 1; then
    echo "ERROR: backend did not become ready at http://localhost:8080/healthz"
    return 1
  fi

  return 0
}

cleanup_stack() {
  if [[ "${STACK_STARTED_BY_SCRIPT}" -eq 1 ]] && have_docker_compose; then
    echo "=== Stopping docker compose stack started by run_tests.sh ==="
    docker compose -f "${ROOT_DIR}/docker-compose.yml" down -v >/dev/null 2>&1 || true
  fi
}

trap cleanup_stack EXIT

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
if ! start_stack_for_api_tests_if_needed; then
  echo "=== Final Summary ==="
  echo "TOTAL=1"
  echo "PASSED=0"
  echo "FAILED=1"
  exit 1
fi
run_suite "API_tests" "${ROOT_DIR}/API_tests/run_api_tests.sh"
run_suite "e2e_tests" "${ROOT_DIR}/e2e_tests/run_e2e_tests.sh"

echo "=== Final Summary ==="
echo "TOTAL=${TOTAL}"
echo "PASSED=${PASSED}"
echo "FAILED=${FAILED}"

if [[ ${FAILED} -gt 0 ]]; then
  exit 1
fi
exit 0
