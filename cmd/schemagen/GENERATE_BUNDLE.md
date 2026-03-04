# Generate Self-Contained Schema Bundle

Run from `cmd/schemagen`:

```bash
cd /Users/israel.blancas/projects/contrib6/cmd/schemagen

go run ./bundlegen \
  -all \
  -inline \
  -allow-missing \
  -out /tmp/collector-bundle-inline.yaml \
  -t yaml \
  -map github.com/open-telemetry/opentelemetry-collector-contrib=/Users/israel.blancas/projects/contrib6 \
  -map go.opentelemetry.io/collector=/Users/israel.blancas/projects/collecto6
```

# Core + Coralogix Bundle (Strict)

```bash
cd /Users/israel.blancas/projects/contrib6/cmd/schemagen

# Ensure core repo has schemas for nop components (one-time).
go run ./sweep \
  -root /Users/israel.blancas/projects/collecto6 \
  -schema-type yaml \
  -report /tmp/schemagen-core-missing.yaml

# Generate internal healthcheck schemas used by health_check extension.
go run . -t yaml /Users/israel.blancas/projects/contrib6/internal/healthcheck/internal/http
go run . -t yaml /Users/israel.blancas/projects/contrib6/internal/healthcheck/internal/grpc
go run . -t yaml /Users/israel.blancas/projects/contrib6/internal/healthcheck/internal/common
go run . -t yaml /Users/israel.blancas/projects/contrib6/internal/healthcheck
go run . -t yaml /Users/israel.blancas/projects/contrib6/extension/healthcheckextension

# Bundle config covering core + coralogix + health_check/pprof/zpages.
cat > /tmp/bundle-core-coralogix.yaml <<'EOF'
receivers:
  otlp: {}
  nop: {}

processors:
  batch: {}
  memory_limiter: {}
  coralogix: {}

exporters:
  debug: {}
  nop: {}
  otlp_grpc: {}
  otlp_http: {}
  coralogix: {}

extensions:
  zpages: {}
  health_check: {}
  pprof: {}
EOF

go run ./bundlegen \
  -config /tmp/bundle-core-coralogix.yaml \
  -inline \
  -out /tmp/otel-core-coralogix-bundle.json \
  -t json \
  -map github.com/open-telemetry/opentelemetry-collector-contrib=/Users/israel.blancas/projects/contrib6 \
  -map go.opentelemetry.io/collector=/Users/israel.blancas/projects/collecto6
```

# Full Core + Contrib Bundle (Self-Contained)

```bash
cd /Users/israel.blancas/projects/contrib6/cmd/schemagen

./generate-full-bundle.sh
```

Environment overrides:
```
CONTRIB_ROOT=/path/to/opentelemetry-collector-contrib
CORE_ROOT=/path/to/opentelemetry-collector
OUT_PATH=/tmp/otel-collector-full-bundle.json
REPORT_DIR=/tmp
```
