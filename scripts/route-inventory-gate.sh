#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"
REPORT_DIR=${ROUTE_REPORT_DIR:-reports/quality/routes}
mkdir -p "$REPORT_DIR"
LOG="$REPORT_DIR/route-inventory-go-test.log"
REPORT="$REPORT_DIR/latest.json"

status=0
go test ./internal/router -run 'TestRouteInventory|TestProtectedRoutesRejectMissingToken|TestCORSAllowedOriginEchoOnly' -count=1 -json 2>&1 | tee "$LOG" || status=${PIPESTATUS[0]}
python3 - "$LOG" "$REPORT" "$status" <<'PY'
import json, sys, time
from pathlib import Path
log, report, status = sys.argv[1], sys.argv[2], int(sys.argv[3])
passed=[]; failed=[]
for line in Path(log).read_text(errors='replace').splitlines():
    try:
        obj=json.loads(line)
    except Exception:
        continue
    if obj.get('Action')=='pass' and obj.get('Test'):
        passed.append(obj['Test'])
    if obj.get('Action')=='fail' and obj.get('Test'):
        failed.append(obj['Test'])
out={
    'generated_at_unix': int(time.time()),
    'status': 'PASS' if status == 0 and not failed else 'FAIL',
    'passed_tests': sorted(set(passed)),
    'failed_tests': sorted(set(failed)),
    'log': log,
}
Path(report).write_text(json.dumps(out, indent=2, ensure_ascii=False)+'\n')
print(json.dumps({'status': out['status'], 'passed_tests': out['passed_tests'], 'failed_tests': out['failed_tests']}, ensure_ascii=False))
raise SystemExit(0 if out['status']=='PASS' else 1)
PY
