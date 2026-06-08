#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

REPORT_DIR="$ROOT_DIR/reports/quality/contracts"
REPORT_PATH="$REPORT_DIR/openapi-drift-latest.json"
BASELINE_PATH="${OPENAPI_BASELINE:-$ROOT_DIR/docs/openapi/baseline.openapi.json}"
GENERATED_PATH="${OPENAPI_GENERATED:-$ROOT_DIR/docs/openapi/openapi.json}"

mkdir -p "$REPORT_DIR"
./scripts/gen-swagger.sh >/tmp/ecommerce-gen-swagger-latest.log

python3 - "$GENERATED_PATH" "$BASELINE_PATH" "$REPORT_PATH" "/tmp/ecommerce-gen-swagger-latest.log" <<'PY'
import json
import shutil
import sys
import time
from pathlib import Path

current_path = Path(sys.argv[1])
baseline_path = Path(sys.argv[2])
report_path = Path(sys.argv[3])
generation_log = Path(sys.argv[4])

if not current_path.exists():
    payload = {
        "generated_at_unix": int(time.time()),
        "status": "FAIL",
        "generated_spec": str(current_path),
        "baseline_spec": str(baseline_path),
        "breaking_changes": [{"type": "generated_spec_missing", "path": str(current_path)}],
        "warnings": [],
    }
    report_path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(json.dumps({"status": payload["status"], "report": str(report_path), "breaking_changes": payload["breaking_changes"]}, ensure_ascii=False))
    raise SystemExit(1)

if not baseline_path.exists():
    baseline_path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(current_path, baseline_path)
    current = json.loads(current_path.read_text(encoding="utf-8"))
    payload = {
        "generated_at_unix": int(time.time()),
        "status": "PASS",
        "baseline_created": True,
        "policy": "No committed OpenAPI baseline existed; current generated spec was promoted to baseline so future path/method/envelope removals are gated.",
        "generated_spec": str(current_path),
        "baseline_spec": str(baseline_path),
        "path_count": len(current.get("paths", {})),
        "breaking_changes": [],
        "warnings": ["baseline_created_from_current_generated_spec"],
        "generation_log": generation_log.read_text(encoding="utf-8", errors="replace") if generation_log.exists() else "",
    }
    report_path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(json.dumps({"status": payload["status"], "report": str(report_path), "baseline_created": True, "baseline_spec": str(baseline_path)}, ensure_ascii=False))
    raise SystemExit(0)

current = json.loads(current_path.read_text(encoding="utf-8"))
baseline = json.loads(baseline_path.read_text(encoding="utf-8"))

def _has_envelope_ref(value):
    if isinstance(value, dict):
        if value.get("$ref") == "#/components/schemas/ResponseEnvelope":
            return True
        return any(_has_envelope_ref(v) for v in value.values())
    if isinstance(value, list):
        return any(_has_envelope_ref(v) for v in value)
    return False

breaking = []
warnings = []
http_methods = {"get", "post", "put", "patch", "delete", "head", "options", "trace"}
base_paths = baseline.get("paths", {}) or {}
cur_paths = current.get("paths", {}) or {}

for path, base_ops in sorted(base_paths.items()):
    if path not in cur_paths:
        breaking.append({"type": "removed_path", "path": path})
        continue
    cur_ops = cur_paths.get(path, {}) or {}
    for method in sorted(k for k in base_ops.keys() if k.lower() in http_methods):
        if method not in cur_ops:
            breaking.append({"type": "removed_method", "path": path, "method": method.upper()})
            continue
        base_responses = (base_ops.get(method, {}) or {}).get("responses", {}) or {}
        cur_responses = (cur_ops.get(method, {}) or {}).get("responses", {}) or {}
        if any(_has_envelope_ref(resp) for resp in base_responses.values()) and not any(_has_envelope_ref(resp) for resp in cur_responses.values()):
            breaking.append({"type": "response_envelope_schema_removed", "path": path, "method": method.upper()})

base_schemas = (((baseline.get("components") or {}).get("schemas") or {}))
cur_schemas = (((current.get("components") or {}).get("schemas") or {}))
base_env = base_schemas.get("ResponseEnvelope") or base_schemas.get("responseEnvelope") or {}
cur_env = cur_schemas.get("ResponseEnvelope") or {}
if base_env:
    if not cur_env:
        breaking.append({"type": "response_envelope_schema_removed", "schema": "ResponseEnvelope"})
    else:
        base_required = list(base_env.get("required") or [])
        cur_required = list(cur_env.get("required") or [])
        cur_required_set = set(cur_required)
        cur_required_lower = {str(item).lower(): item for item in cur_required}
        for field in base_required:
            if field not in cur_required_set:
                replacement = cur_required_lower.get(str(field).lower())
                breaking.append({"type": "required_field_removed_or_recased", "schema": "ResponseEnvelope", "field": field, "current_casing": replacement})
        base_props = base_env.get("properties") or {}
        cur_props = cur_env.get("properties") or {}
        cur_props_lower = {str(k).lower(): k for k in cur_props.keys()}
        for field in base_props.keys():
            if field not in cur_props:
                replacement = cur_props_lower.get(str(field).lower())
                severity = "breaking" if field in base_required else "warning"
                entry = {"type": "field_removed_or_recased", "schema": "ResponseEnvelope", "field": field, "current_casing": replacement}
                (breaking if severity == "breaking" else warnings).append(entry)

added_paths = sorted(set(cur_paths) - set(base_paths))
if added_paths:
    warnings.append({"type": "added_paths", "count": len(added_paths), "paths": added_paths[:50]})

payload = {
    "generated_at_unix": int(time.time()),
    "status": "FAIL" if breaking else "PASS",
    "baseline_created": False,
    "policy": "Breaking drift fails on removed path, removed method, removed response envelope, or required response-envelope field/casing removal.",
    "generated_spec": str(current_path),
    "baseline_spec": str(baseline_path),
    "path_count": len(cur_paths),
    "baseline_path_count": len(base_paths),
    "breaking_changes": breaking,
    "warnings": warnings,
    "generation_log": generation_log.read_text(encoding="utf-8", errors="replace") if generation_log.exists() else "",
}
report_path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
print(json.dumps({"status": payload["status"], "report": str(report_path), "breaking_change_count": len(breaking), "warning_count": len(warnings)}, ensure_ascii=False))
raise SystemExit(1 if breaking else 0)
PY
