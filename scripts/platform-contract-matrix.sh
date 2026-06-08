#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
REPORT_DIR="$ROOT_DIR/reports/quality/contracts"
REPORT_PATH="$REPORT_DIR/platform-contract-matrix-latest.json"
mkdir -p "$REPORT_DIR"

python3 - "$ROOT_DIR" "$REPORT_PATH" <<'PY'
import json
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

root = Path(sys.argv[1])
report_path = Path(sys.argv[2])

def run_gate(name: str, cmd: list[str], timeout: int = 300) -> dict[str, Any]:
    started = time.time()
    try:
        proc = subprocess.run(cmd, cwd=root, text=True, capture_output=True, timeout=timeout)
        return {
            "name": name,
            "command": " ".join(cmd),
            "exit_code": proc.returncode,
            "elapsed_ms": int((time.time() - started) * 1000),
            "stdout_tail": proc.stdout[-4000:],
            "stderr_tail": proc.stderr[-4000:],
        }
    except subprocess.TimeoutExpired as exc:
        return {
            "name": name,
            "command": " ".join(cmd),
            "exit_code": 124,
            "elapsed_ms": int((time.time() - started) * 1000),
            "stdout_tail": (exc.stdout or "")[-4000:] if isinstance(exc.stdout, str) else "",
            "stderr_tail": (exc.stderr or "")[-4000:] if isinstance(exc.stderr, str) else "",
            "timeout": timeout,
        }

def load_json(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {"status": "NOT_RUN", "path": str(path), "missing": True}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
        if isinstance(data, dict):
            data.setdefault("path", str(path))
            return data
        return {"status": "FAIL", "path": str(path), "error": "json_root_not_object"}
    except Exception as exc:
        return {"status": "FAIL", "path": str(path), "error_type": type(exc).__name__, "error": str(exc)}

def norm_status(value: Any) -> str:
    return str(value or "UNKNOWN").upper()

runs = []
runs.append(run_gate("platform-contract-gate", ["./scripts/platform-contract-gate.sh"], timeout=300))
runs.append(run_gate("frontend-consumer-sweep", ["./scripts/frontend-consumer-sweep.sh"], timeout=120))

platform_contract = load_json(root / "reports/quality/platform-contract/latest.json")
frontend_consumer = load_json(root / "reports/quality/contracts/frontend-consumer-sweep-latest.json")
openapi_drift = load_json(root / "reports/quality/contracts/openapi-drift-latest.json")
business_journey = load_json(root / "reports/quality/business-journeys/latest.json")
api_contract_journey = load_json(root / "reports/quality/business-journeys/api-contract-latest.json")

# Prefer executed local harness evidence when available. Prod refusal is preserved as a note/waiver, not hidden.
local_smoke_source = None
for candidate_name, candidate in (("api_contract", api_contract_journey), ("business_journey", business_journey)):
    if norm_status(candidate.get("status")) == "PASS" and candidate.get("env") == "local":
        local_smoke_source = candidate_name
        break

live_notes = []
prod_live_smoke = business_journey.get("prod_live_smoke") or api_contract_journey.get("prod_live_smoke") or "NOT_RUN"
if prod_live_smoke == "NOT_RUN":
    live_notes.append("prod_live_smoke NOT_RUN by policy; prod requires separately approved runbook/evidence.")
if norm_status(business_journey.get("status")) == "BLOCKED" and business_journey.get("env") == "prod":
    live_notes.append(f"business journey latest is prod refusal: {business_journey.get('reason')}")

if local_smoke_source:
    live_smoke_status = "PASS"
else:
    statuses = {norm_status(api_contract_journey.get("status")), norm_status(business_journey.get("status"))}
    if "FAIL" in statuses or "BLOCKED" in statuses:
        live_smoke_status = "NEEDS_HUMAN"
    elif statuses & {"PASS_WITH_NOTES"}:
        live_smoke_status = "PASS_WITH_NOTES"
    else:
        live_smoke_status = "NOT_RUN"

waivers = []
for item in frontend_consumer.get("waivers", []) or []:
    waivers.append({"source": "frontend_consumer", **item})
if prod_live_smoke == "NOT_RUN":
    waivers.append({"source": "live_smoke", "scope": "prod", "reason": "prod live smoke not required for local/dev contract candidate; separate prod runbook approval required."})

contract_failures = []
if norm_status(platform_contract.get("status")) != "PASS":
    contract_failures.append({"gate": "platform_contract", "status": platform_contract.get("status")})
if norm_status(frontend_consumer.get("status")) != "PASS":
    contract_failures.append({"gate": "frontend_consumer", "status": frontend_consumer.get("status"), "failures": frontend_consumer.get("failures", [])})
if norm_status(openapi_drift.get("status")) not in {"PASS", "PASS_WITH_NOTES"}:
    contract_failures.append({"gate": "openapi_drift", "status": openapi_drift.get("status"), "breaking_changes": openapi_drift.get("breaking_changes", [])})
for run in runs:
    if run["exit_code"] != 0:
        contract_failures.append({"gate": run["name"], "status": "FAIL", "exit_code": run["exit_code"]})

if contract_failures:
    decision = "CANNOT_DEPLOY"
elif live_smoke_status == "PASS":
    decision = "CAN_DEPLOY"
else:
    decision = "NEEDS_HUMAN"

payload = {
    "generated_at_unix": int(time.time()),
    "status": "PASS" if decision == "CAN_DEPLOY" else ("FAIL" if decision == "CANNOT_DEPLOY" else "PASS_WITH_NOTES"),
    "decision": decision,
    "producer": "ecommerce-backend",
    "candidate_scope": "local/dev",
    "policy": "CAN_DEPLOY requires producer OpenAPI drift PASS, platform contract PASS, frontend consumer sweep PASS, and local smoke PASS. Prod live smoke may be waived for local/dev but is recorded explicitly.",
    "producer_contract": openapi_drift,
    "platform_contract": platform_contract,
    "frontend_consumer": frontend_consumer,
    "live_smoke": {
        "status": live_smoke_status,
        "local_smoke_source": local_smoke_source,
        "prod_live_smoke": prod_live_smoke,
        "business_journey_report": business_journey,
        "api_contract_journey_report": api_contract_journey,
        "notes": live_notes,
    },
    "waivers": waivers,
    "runs": runs,
    "failures": contract_failures,
}
report_path.write_text(json.dumps(payload, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
print(json.dumps({"status": payload["status"], "decision": decision, "report": str(report_path), "platform_contract": platform_contract.get("status"), "frontend_consumer": frontend_consumer.get("status"), "openapi_drift": openapi_drift.get("status"), "live_smoke": live_smoke_status}, ensure_ascii=False))
raise SystemExit(1 if decision == "CANNOT_DEPLOY" else 0)
PY
