#!/usr/bin/env bash
set -euo pipefail

CONTRIB_ROOT="${CONTRIB_ROOT:-/Users/israel.blancas/projects/contrib6}"
CORE_ROOT="${CORE_ROOT:-/Users/israel.blancas/projects/collecto6}"
OUT_PATH="${OUT_PATH:-/tmp/otel-collector-full-bundle.json}"
REPORT_DIR="${REPORT_DIR:-/tmp}"
FORCE_REGEN="${FORCE_REGEN:-0}"

#cd "$CONTRIB_ROOT/cmd/schemagen"

# Refresh component schemas from metadata (best-effort)
force_flag=()
if [[ "$FORCE_REGEN" == "1" || "$FORCE_REGEN" == "true" ]]; then
  force_flag=(-force)
fi
pwd
go run ./sweep \
  "${force_flag[@]}" \
  -root "$CORE_ROOT" \
  -schema-type yaml \
  -report "$REPORT_DIR/schemagen-core-missing.yaml"

go run ./sweep \
  "${force_flag[@]}" \
  -root "$CONTRIB_ROOT" \
  -schema-type yaml \
  -report "$REPORT_DIR/schemagen-contrib-missing.yaml"

regen_from_ref() {
  local ref="$1"
  local pkg="${ref%.*}"
  if [[ -d "$pkg" ]]; then
    echo "[fix] schemagen $pkg"
    go run . -t yaml "$pkg" >/dev/null
  else
    echo "[warn] ref dir not found: $pkg" >&2
  fi
}

regen_from_schema() {
  local schema="$1"
  local dir
  dir="$(dirname "$schema")"
  if [[ -d "$dir" ]]; then
    echo "[fix] schemagen $dir"
    go run . -t yaml "$dir" >/dev/null
  else
    echo "[warn] schema dir not found: $dir" >&2
  fi
}

extract_ref() {
  echo "$1" | sed -n 's/.*schema not found for ref \"\\([^\"]*\\)\".*/\\1/p' | head -n1
}

extract_schema_from_missing_defs() {
  echo "$1" | grep 'missing \$defs' | sed -n 's/.*schema "\([^"]*\)".*/\1/p' | head -n1
}

max=80
for ((i=1; i<=max; i++)); do
  set +e
  out=$(go run ./bundlegen -all -inline -out "$OUT_PATH" -t json \
    -map github.com/open-telemetry/opentelemetry-collector-contrib="$CONTRIB_ROOT" \
    -map go.opentelemetry.io/collector="$CORE_ROOT" 2>&1)
  rc=$?
  set -e

  if [[ $rc -eq 0 ]]; then
    echo "bundle written to $OUT_PATH"
    exit 0
  fi

  echo "$out" >&2

  ref="$(extract_ref "$out")"
  if [[ -n "$ref" ]]; then
    regen_from_ref "$ref"
    continue
  fi

  schema="$(extract_schema_from_missing_defs "$out")"
  if [[ -n "$schema" ]]; then
    regen_from_schema "$schema"
    continue
  fi

  echo "[fatal] unhandled error from bundlegen" >&2
  exit 1

done

echo "[fatal] exceeded $max attempts" >&2
exit 1
