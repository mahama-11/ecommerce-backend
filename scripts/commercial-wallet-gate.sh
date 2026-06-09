#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"
REPORT_DIR=${COMMERCIAL_WALLET_REPORT_DIR:-reports/quality/commercial-wallet}
mkdir -p "$REPORT_DIR"
LOG="$REPORT_DIR/go-test-commercial-wallet.log"
REPORT="$REPORT_DIR/latest.json"
status=0
go test ./internal/modules/wallet ./internal/modules/commercial ./internal/modules/billing ./internal/modules/commission ./internal/modules/promotion ./internal/repository -count=1 -cover 2>&1 | tee "$LOG" || status=${PIPESTATUS[0]}
python3 - "$LOG" "$REPORT" "$status" <<'PY'
import json, re, sys, time
from pathlib import Path
log, report, status = sys.argv[1], sys.argv[2], int(sys.argv[3])
text = Path(log).read_text(errors='replace') if Path(log).exists() else ''
packages = {m.group(1): float(m.group(2)) for m in re.finditer(r'(?m)^ok\s+(\S+)\s+.*coverage:\s+([0-9.]+)% of statements', text)}
required = [
 'ecommerce-service/internal/modules/wallet',
 'ecommerce-service/internal/modules/commercial',
 'ecommerce-service/internal/modules/billing',
 'ecommerce-service/internal/modules/commission',
 'ecommerce-service/internal/modules/promotion',
 'ecommerce-service/internal/repository',
]
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
