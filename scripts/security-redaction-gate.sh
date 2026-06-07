#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$ROOT_DIR"
REPORT_DIR=${SECURITY_REDACTION_REPORT_DIR:-reports/quality/observability-security}
mkdir -p "$REPORT_DIR"
REPORT="$REPORT_DIR/latest.json"
GO_LOG="$REPORT_DIR/go-test-redaction.log"
status=0
go test ./internal/observability ./internal/middleware ./internal/telemetry -run 'Test.*Redact|TestSafeError|TestSlogArgs' -count=1 2>&1 | tee "$GO_LOG" || status=${PIPESTATUS[0]}
RAW_TRACE_MATCHES=$(grep -RInE '\.RecordError\(|SetStatus\([^\n]*err\.Error\(\)|"error", err\.Error\(' internal --include='*.go' | grep -vE 'internal/(telemetry/tracing\.go|observability/observability\.go):' || true)
python3 - "$REPORT" "$GO_LOG" "$status" "$RAW_TRACE_MATCHES" <<'PY'
import json, re, sys, time
from pathlib import Path
report = Path(sys.argv[1]); go_log = Path(sys.argv[2]); go_status = int(sys.argv[3]); raw_trace_matches = sys.argv[4]
observability_report = report.parent / 'observability-latest.json'
raw_bearer = 'Bearer ' + 'eyJsecret.jwt'
raw_db_url = 'postgres://dbuser:' + 'dbpass' + '@192.0.2.10:5432/ecommerce'
fixtures = {
  'authorization': 'Authorization: ' + raw_bearer,
  'token': 'token=raw-token-value secret=raw-secret-value password=raw-password-value',
  'db_url': raw_db_url,
  'ssh_target': 'deploy to root@203.0.113.10 complete',
  'storage_key': 'storage_key=private/provider/raw/object-key provider_key=raw-provider-key',
}
patterns = [
  (re.compile(r'(?i)(Bearer\s+)[A-Za-z0-9._~+/-]+=*'), r'\1[redacted]'),
  (re.compile(r'(?i)((?:token|secret|password|storage_key|provider_key)=)[^\s,;]+'), r'\1[redacted]'),
  (re.compile(r'(?i)((?:postgres|postgresql|mysql)://[^:]+:)[^@\s]+(@)'), r'\1[redacted]\2'),
  (re.compile(r'\b[A-Za-z0-9._%+-]+@(?:\d{1,3}\.){3}\d{1,3}\b'), '[redacted-ssh-target]'),
  (re.compile(r'\b(?:\d{1,3}\.){3}\d{1,3}\b'), '[redacted-ip]'),
]
redacted = {}
for name, value in fixtures.items():
    out = value
    for pattern, replacement in patterns:
        out = pattern.sub(replacement, out)
    redacted[name] = out
forbidden = ['eyJsecret.jwt','raw-token-value','raw-secret-value','raw-password-value','dbpass','192.0.2.10','203.0.113.10','private/provider/raw/object-key','raw-provider-key']
failures = []
joined = json.dumps(redacted, sort_keys=True)
for item in forbidden:
    if item in joined:
        failures.append({'type':'redaction_miss','value':item})
if go_status != 0:
    failures.append({'type':'go_redaction_tests_failed','status':go_status,'log':str(go_log)})
if raw_trace_matches.strip():
    failures.append({'type':'raw_trace_error_recorders_present','matches': raw_trace_matches.splitlines()[:20]})
observability = None
if observability_report.exists():
    observability = json.loads(observability_report.read_text(errors='replace'))
    if observability.get('status') != 'PASS':
        failures.append({'type':'observability_gate_not_pass','status':observability.get('status')})
out = {'generated_at_unix': int(time.time()), 'status': 'PASS' if not failures else 'FAIL', 'observability': observability, 'go_redaction_tests': {'status': go_status, 'log': str(go_log)}, 'redacted_samples': redacted, 'failures': failures}
report.write_text(json.dumps(out, indent=2, ensure_ascii=False)+'\n')
print(json.dumps({'status': out['status'], 'failures': failures}, ensure_ascii=False))
raise SystemExit(0 if out['status'] == 'PASS' else 1)
PY
