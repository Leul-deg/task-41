#!/usr/bin/env bash
set -u

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if command -v python3 >/dev/null 2>&1; then
  exec python3 "${ROOT_DIR}/API_tests/run_api_tests.py"
elif command -v python >/dev/null 2>&1; then
  exec python "${ROOT_DIR}/API_tests/run_api_tests.py"
else
  echo "[api_tests] starting"
  echo "TEST=api_tests.environment"
  echo "STATUS=FAIL"
  echo "REASON=python interpreter not found"
  echo "LOG_SNIPPET=install python3 or python to run API suite"
  echo "---"
  echo "[api_tests] summary"
  echo "SUITE=API_tests"
  echo "TOTAL=1"
  echo "PASSED=0"
  echo "FAILED=1"
  echo "TODO_GAPS=0"
  exit 1
fi
