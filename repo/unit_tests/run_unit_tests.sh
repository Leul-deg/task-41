#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GO_MIN_VERSION="1.23"
GO_UNIT_TEST_IMAGE="${GO_UNIT_TEST_IMAGE:-golang:1.23-alpine}"

TOTAL=0
PASSED=0
FAILED=0

command_exists() {
  command -v "$1" >/dev/null 2>&1
}

get_system_go_version() {
  local token
  local output

  if ! command_exists go; then
    return 1
  fi

  output="$(go version 2>/dev/null || true)"
  for token in ${output}; do
    if [[ "${token}" =~ ^go([0-9]+)\.([0-9]+)(\.([0-9]+))? ]]; then
      echo "${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[4]:-0}"
      return 0
    fi
  done

  return 1
}

version_gte() {
  local current="$1"
  local required="$2"
  local cur_major cur_minor cur_patch
  local req_major req_minor req_patch

  IFS='.' read -r cur_major cur_minor cur_patch <<<"${current}"
  IFS='.' read -r req_major req_minor req_patch <<<"${required}"

  cur_patch="${cur_patch:-0}"
  req_patch="${req_patch:-0}"

  if ((cur_major > req_major)); then
    return 0
  fi
  if ((cur_major < req_major)); then
    return 1
  fi
  if ((cur_minor > req_minor)); then
    return 0
  fi
  if ((cur_minor < req_minor)); then
    return 1
  fi
  if ((cur_patch >= req_patch)); then
    return 0
  fi

  return 1
}

is_system_go_at_least() {
  local required_version="$1"
  local system_version

  system_version="$(get_system_go_version)" || return 1
  version_gte "${system_version}" "${required_version}"
}

run_go_test_in_docker() {
  local rel_dir="$1"
  local log_file="$2"

  MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*' docker run --rm -v "${ROOT_DIR}/${rel_dir}:/src" -w /src "${GO_UNIT_TEST_IMAGE}" /usr/local/go/bin/go test ./... >"${log_file}" 2>&1
}

run_go_test_in_dir() {
  local rel_dir="$1"
  local log_file="$2"
  local system_version

  if [[ "${MERIDIAN_USE_DOCKER_GO:-0}" == "1" ]] && command_exists docker; then
    run_go_test_in_docker "${rel_dir}" "${log_file}"
    return $?
  fi

  if is_system_go_at_least "${GO_MIN_VERSION}"; then
    (cd "${ROOT_DIR}/${rel_dir}" && go test ./... >"${log_file}" 2>&1)
    return $?
  fi

  if command_exists docker; then
    run_go_test_in_docker "${rel_dir}" "${log_file}"
    return $?
  fi

  if command_exists go; then
    system_version="$(get_system_go_version || true)"
    if [[ -n "${system_version}" ]]; then
      echo "system go ${system_version} is too old (need >= ${GO_MIN_VERSION}) and docker is unavailable" >"${log_file}"
    else
      echo "system go was found but version could not be parsed; docker is unavailable" >"${log_file}"
    fi
  else
    echo "go toolchain and docker are both unavailable" >"${log_file}"
  fi
  return 1
}

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

  run_go_test_in_dir "${rel_dir}" "${log_file}"

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
