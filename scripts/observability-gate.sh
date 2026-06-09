#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"
REPORT_DIR=${OBSERVABILITY_REPORT_DIR:-reports/quality/observability-security}
mkdir -p "$REPORT_DIR"
LOG="$REPORT_DIR/go-test-observability.log"
REPORT="$REPORT_DIR/observability-latest.json"
status=0
go test ./internal/observability ./internal/telemetry ./internal/middleware ./internal/platform ./pkg/response ./pkg/metrics -count=1 -cover 2>&1 | tee "$LOG" || status=${PIPESTATUS[0]}
python3 - "$LOG" "$REPORT" "$status" <<'PY'
import json, re, sys, time
from pathlib import Path
log, report, status = sys.argv[1], sys.argv[2], int(sys.argv[3])
text = Path(log).read_text(errors='replace') if Path(log).exists() else ''
packages = {m.group(1): float(m.group(2)) for m in re.finditer(r'(?m)^ok\s+(\S+)\s+.*coverage:\s+([0-9.]+)% of statements', text)}
required = ['ecommerce-service/internal/observability','ecommerce-service/internal/telemetry','ecommerce-service/internal/middleware','ecommerce-service/internal/platform','ecommerce-service/pkg/response','ecommerce-service/pkg/metrics']
failures = []
if status != 0 or re.search(r'(?m)^--- FAIL:|^FAIL\s+', text):
    failures.append({'type':'go_test','status':status})
for pkg in required:
    if pkg not in packages:
        failures.append({'type':'coverage_missing','package':pkg})
out = {'generated_at_unix': int(time.time()), 'status': 'PASS' if not failures else 'FAIL', 'packages': packages, 'failures': failures, 'log': log}
Path(report).write_text(json.dumps(out, indent=2, ensure_ascii=False)+'\n')
print(json.dumps({'status': out['status'], 'packages': packages, 'failures': failures}, ensure_ascii=False))
raise SystemExit(0 if out['status'] == 'PASS' else 1)
PY
