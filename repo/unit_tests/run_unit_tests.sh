#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

TOTAL=0
PASSED=0
FAILED=0

print_result() {
  local name="$1"
  local status="$2"
  local reason="$3"
  local snippet="$4"
  echo "TEST=${name}"
  echo "STATUS=${status}"
  if [[ -n "${reason}" ]]; then
    echo "REASON=${reason}"
  fi
  if [[ -n "${snippet}" ]]; then
    echo "LOG_SNIPPET=${snippet}"
  fi
  echo "---"
}

run_group() {
  local name="$1"
  local rel_dir="$2"
  local log_file
  log_file="$(mktemp)"

  TOTAL=$((TOTAL + 1))

  if command -v go >/dev/null 2>&1; then
    (cd "${ROOT_DIR}/${rel_dir}" && go test ./... >"${log_file}" 2>&1)
  elif command -v docker >/dev/null 2>&1; then
    MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' docker run --rm -v "${ROOT_DIR}/${rel_dir}:/src" -w /src golang:1.23-alpine /usr/local/go/bin/go test ./... >"${log_file}" 2>&1
  else
    echo "go toolchain and docker are both unavailable" >"${log_file}"
    false
  fi

  if [[ $? -eq 0 ]]; then
    PASSED=$((PASSED + 1))
    print_result "${name}" "PASS" "" ""
  else
    FAILED=$((FAILED + 1))
    local snippet
    snippet="$(tail -n 8 "${log_file}" | tr '\n' '|' | cut -c1-1200)"
    if [[ -z "${snippet}" ]]; then
      snippet="(no output)"
    fi
    print_result "${name}" "FAIL" "unit test command failed" "${snippet}"
  fi

  rm -f "${log_file}"
}

echo "[unit_tests] starting"
run_group "backend.unit" "backend"
run_group "frontend.unit" "frontend"

echo "[unit_tests] summary"
echo "SUITE=unit_tests"
echo "TOTAL=${TOTAL}"
echo "PASSED=${PASSED}"
echo "FAILED=${FAILED}"

if [[ ${FAILED} -gt 0 ]]; then
  exit 1
fi
exit 0
