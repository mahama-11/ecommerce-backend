#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND_ROOT="${FRONTEND_ROOT:-/root/work/v/ecommerce-frontend}"
SERVICES_DIR="$FRONTEND_ROOT/src/services"
WAIVERS_PATH="${FRONTEND_CONSUMER_WAIVERS:-$ROOT_DIR/docs/contracts/frontend-consumer-waivers.json}"
REPORT_DIR="$ROOT_DIR/reports/quality/contracts"
REPORT_PATH="$REPORT_DIR/frontend-consumer-sweep-latest.json"
ROUTER_FILE="$ROOT_DIR/internal/router/router.go"
mkdir -p "$REPORT_DIR"

python3 - "$SERVICES_DIR" "$WAIVERS_PATH" "$REPORT_PATH" "$ROUTER_FILE" <<'PY'
import json
import re
import sys
import time
from pathlib import Path

services_dir = Path(sys.argv[1])
waivers_path = Path(sys.argv[2])
report_path = Path(sys.argv[3])
router_file = Path(sys.argv[4])
generated_openapi_path = router_file.parents[2] / "docs" / "openapi" / "openapi.json" if len(router_file.parents) > 2 else Path("docs/openapi/openapi.json")
try:
    generated_openapi = json.loads(generated_openapi_path.read_text(encoding="utf-8")) if generated_openapi_path.exists() else {}
except Exception:
    generated_openapi = {}

critical_routes = [
    {"id": "auth-register", "method": "POST", "path": "/api/v1/ecommerce/auth/register"},
    {"id": "auth-login", "method": "POST", "path": "/api/v1/ecommerce/auth/login"},
    {"id": "auth-session", "method": "GET", "path": "/api/v1/ecommerce/auth/session"},
    {"id": "access-me", "method": "GET", "path": "/api/v1/ecommerce/access/me"},
    {"id": "wallet-summary", "method": "GET", "path": "/api/v1/ecommerce/wallet/summary"},
    {"id": "wallet-history", "method": "GET", "path": "/api/v1/ecommerce/wallet/history"},
    {"id": "commercial-offerings", "method": "GET", "path": "/api/v1/ecommerce/commercial/offerings"},
    {"id": "commercial-order-create", "method": "POST", "path": "/api/v1/ecommerce/commercial/orders"},
    {"id": "commercial-order-confirm-payment", "method": "POST", "path": "/api/v1/ecommerce/commercial/orders/:orderID/confirm-payment"},
    {"id": "billing-summary", "method": "GET", "path": "/api/v1/ecommerce/billing/summary"},
    {"id": "billing-charges", "method": "GET", "path": "/api/v1/ecommerce/billing/charges"},
    {"id": "product-create", "method": "POST", "path": "/api/v1/ecommerce/products"},
    {"id": "product-detail", "method": "GET", "path": "/api/v1/ecommerce/products/:product_id"},
    {"id": "source-asset", "method": "POST", "path": "/api/v1/ecommerce/assets/source"},
    {"id": "prompt-preview", "method": "POST", "path": "/api/v1/ecommerce/prompts/preview"},
    {"id": "visual-product-session", "method": "POST", "path": "/api/v1/ecommerce/products/:product_id/v2/visual-sessions"},
    {"id": "visual-session-create", "method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/sessions"},
    {"id": "visual-session-detail", "method": "GET", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id"},
    {"id": "visual-stage-view", "method": "GET", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/stage-view"},
    {"id": "visual-source-reference-create", "method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/source-references"},
    {"id": "visual-source-reference-delete", "method": "DELETE", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/source-references/:source_reference_id"},
    {"id": "visual-deconstruction-job", "method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/deconstruction-jobs"},
    {"id": "visual-deconstruction-element-update", "method": "PATCH", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/deconstruction-elements/:element_id"},
    {"id": "visual-generation-version-create", "method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions"},
    {"id": "visual-generation-version-list", "method": "GET", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions"},
    {"id": "visual-generation-fanout", "method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/generation-version-fanouts"},
    {"id": "visual-generation-select", "method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions/:version_id/select"},
    {"id": "visual-generation-writeback", "method": "POST", "path": "/api/v1/ecommerce/v2/visual-workflows/:session_id/generation-versions/:version_id/writeback-selected-asset"},
    {"id": "export-package-create", "method": "POST", "path": "/api/v1/ecommerce/export-packages"},
    {"id": "download-content", "method": "GET", "path": "/api/v1/ecommerce/downloads/:download_id/content"},
    {"id": "image-job-create", "method": "POST", "path": "/api/v1/ecommerce/image-jobs"},
    {"id": "image-job-list", "method": "GET", "path": "/api/v1/ecommerce/image-jobs"},
    {"id": "image-job-detail", "method": "GET", "path": "/api/v1/ecommerce/image-jobs/:jobID"},
    {"id": "image-job-cancel", "method": "POST", "path": "/api/v1/ecommerce/image-jobs/:jobID/cancel"},
    {"id": "image-asset-content", "method": "GET", "path": "/api/v1/ecommerce/assets/:assetID/content"},
    {"id": "internal-runtime-callback", "method": "POST", "path": "/internal/v1/ecommerce/jobs/:jobID/runtime"},
    {"id": "internal-results-callback", "method": "POST", "path": "/internal/v1/ecommerce/jobs/:jobID/results"},
]

def load_waivers(path: Path) -> dict[tuple[str, str], dict]:
    if not path.exists():
        return {}
    data = json.loads(path.read_text(encoding="utf-8"))
    out = {}
    for item in data.get("waivers", []):
        method = str(item.get("method", "")).upper()
        route = str(item.get("path", ""))
        reason = str(item.get("reason", "")).strip()
        if method and route and reason:
            out[(method, route)] = item
    return out

def backend_route_exists(path: str, method: str) -> bool:
    oa_path = re.sub(r"(?<=/):([A-Za-z_][A-Za-z0-9_]*)", r"{\1}", path)
    spec_paths = generated_openapi.get("paths") if isinstance(generated_openapi, dict) else None
    if isinstance(spec_paths, dict) and method.lower() in (spec_paths.get(oa_path) or {}):
        return True
    if not router_file.exists():
        return False
    text = router_file.read_text(encoding="utf-8", errors="ignore")
    suffix = path
    for prefix in ("/api/v1/ecommerce", "/internal/v1/ecommerce"):
        if suffix.startswith(prefix):
            suffix = suffix[len(prefix):] or "/"
            break
    return f'.{method}("{suffix}"' in text or f'{method}("{suffix}"' in text

def expand_constants(source: str) -> str:
    constants = {}
    for m in re.finditer(r"\bconst\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(['\"])(.*?)\2", source):
        value = m.group(3)
        if value.startswith("/api/") or value.startswith("/internal/"):
            constants[m.group(1)] = value
    changed = True
    while changed:
        changed = False
        for name, value in constants.items():
            for token in ("${" + name + "}", name):
                # Replace template interpolation safely; bare identifier replacement is only useful in template starts like `${VWF}`.
                if token.startswith("${") and token in source:
                    source = source.replace(token, value)
                    changed = True
    return source

def route_regex(path: str) -> re.Pattern:
    parts = []
    for piece in re.split(r"(:[A-Za-z_][A-Za-z0-9_]*)", path):
        if piece.startswith(":"):
            parts.append(r"(?:\$\{[^}]+\}|[^/'\"`?]+)")
        else:
            parts.append(re.escape(piece))
    return re.compile("".join(parts) + r"(?:\?[^'\"`]*)?")

def line_for(source: str, pos: int) -> int:
    return source.count("\n", 0, pos) + 1

if not services_dir.exists():
    payload = {
        "generated_at_unix": int(time.time()),
        "status": "FAIL",
        "frontend_services_dir": str(services_dir),
        "failures": [{"type": "frontend_services_missing", "path": str(services_dir)}],
        "routes": [],
    }
    report_path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(json.dumps({"status": "FAIL", "report": str(report_path), "reason": "frontend services dir missing"}, ensure_ascii=False))
    raise SystemExit(1)

service_files = sorted(services_dir.rglob("*.ts"))
combined = ""
file_offsets = []
for file in service_files:
    text = file.read_text(encoding="utf-8", errors="ignore")
    expanded = expand_constants(text)
    file_offsets.append((len(combined), file, expanded))
    combined += f"\n// FILE: {file}\n" + expanded

waivers = load_waivers(waivers_path)
route_results = []
failures = []
used_waivers = []

for route in critical_routes:
    method = route["method"].upper()
    path = route["path"]
    pattern = route_regex(path)
    evidence = None
    fallback_evidence = None
    for match in pattern.finditer(combined):
        # Find the originating service file from the concatenated offset.
        file_path = None
        file_line = None
        for start, file, expanded in reversed(file_offsets):
            if match.start() >= start:
                file_path = file
                file_line = line_for(combined[start:match.start()], match.start() - start)
                break
        snippet_start = max(0, match.start() - 160)
        snippet_end = min(len(combined), match.end() + 160)
        snippet = combined[snippet_start:snippet_end]
        explicit_method = re.search(r"method\s*:\s*['\"]([A-Z]+)['\"]", snippet)
        method_evidence = (explicit_method.group(1) == method) if explicit_method else (method == "GET")
        candidate = {
            "file": str(file_path) if file_path else None,
            "line": file_line,
            "matched_path_fragment": match.group(0),
            "method_evidence": bool(method_evidence),
        }
        if fallback_evidence is None:
            fallback_evidence = candidate
        if method_evidence:
            evidence = candidate
            break
    if evidence is None and fallback_evidence and method == "GET":
        # Dynamic GET helpers sometimes share snippets with neighboring method declarations;
        # keep a weaker evidence record rather than falsely reporting no consumer.
        evidence = fallback_evidence
    waiver = waivers.get((method, path))
    exists = backend_route_exists(path, method)
    status = "PASS" if evidence else ("WAIVED" if waiver else "FAIL")
    if status == "FAIL":
        failures.append({"type": "missing_frontend_consumer", "id": route["id"], "method": method, "path": path, "backend_route_exists": exists})
    if waiver and not evidence:
        used_waivers.append({"id": route["id"], "method": method, "path": path, "reason": waiver.get("reason")})
    route_results.append({**route, "backend_route_exists": exists, "status": status, "consumer": evidence, "waiver": waiver if status == "WAIVED" else None})

unused_waivers = []
critical_keys = {(r["method"].upper(), r["path"]) for r in critical_routes}
for key, waiver in sorted(waivers.items()):
    if key not in critical_keys:
        unused_waivers.append({"method": key[0], "path": key[1], "reason": waiver.get("reason")})

payload = {
    "generated_at_unix": int(time.time()),
    "status": "FAIL" if failures else "PASS",
    "policy": "Every Phase3 critical backend route must have a frontend service consumer or an explicit waiver with an internal-only/no-frontend-surface reason.",
    "frontend_services_dir": str(services_dir),
    "service_files_scanned": [str(p) for p in service_files],
    "waivers_path": str(waivers_path),
    "critical_route_count": len(critical_routes),
    "consumed_count": sum(1 for r in route_results if r["status"] == "PASS"),
    "waived_count": sum(1 for r in route_results if r["status"] == "WAIVED"),
    "missing_unwaived_count": len(failures),
    "routes": route_results,
    "waivers": used_waivers,
    "unused_waivers": unused_waivers,
    "failures": failures,
}
report_path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
print(json.dumps({"status": payload["status"], "report": str(report_path), "critical_route_count": len(critical_routes), "consumed_count": payload["consumed_count"], "waived_count": payload["waived_count"], "missing_unwaived_count": len(failures)}, ensure_ascii=False))
raise SystemExit(1 if failures else 0)
PY
