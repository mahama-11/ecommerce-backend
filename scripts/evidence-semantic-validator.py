#!/usr/bin/env python3
"""Semantic release evidence validator for Ecommerce backend quality gates.

This script is intentionally local/dev safe: it only reads reports produced by the
repository gates and refuses to treat hidden/missing prod evidence as a full PASS.
Exit code is non-zero only for missing/failed required local or contract evidence.
A release candidate with all local/contract gates green but prod live smoke NOT_RUN
is reported as PASS_WITH_NOTES with an explicit prod_not_run reason.
"""
from __future__ import annotations

import json
import time
from pathlib import Path
from typing import Any

ROOT = Path(__file__).resolve().parents[1]
REPORT_DIR = ROOT / "reports" / "quality" / "contracts"
REPORT_PATH = REPORT_DIR / "evidence-semantic-validation-latest.json"
BUSINESS_REPORT = ROOT / "reports" / "quality" / "business-journeys" / "latest.json"
API_CONTRACT_REPORT = ROOT / "reports" / "quality" / "business-journeys" / "api-contract-latest.json"
OPENAPI_DRIFT_REPORT = ROOT / "reports" / "quality" / "contracts" / "openapi-drift-latest.json"
FRONTEND_CONSUMER_REPORT = ROOT / "reports" / "quality" / "contracts" / "frontend-consumer-sweep-latest.json"
PLATFORM_MATRIX_REPORT = ROOT / "reports" / "quality" / "contracts" / "platform-contract-matrix-latest.json"

PASS_STATUSES = {"PASS", "CAN_DEPLOY"}
PROD_NOT_RUN_STATUSES = {"NOT_RUN", "BLOCKED", "REFUSED", "PENDING_APPROVAL"}


def norm(value: Any) -> str:
    return str(value or "UNKNOWN").strip().upper()


def load_json(path: Path) -> dict[str, Any]:
    if not path.exists():
        return {"status": "NOT_RUN", "path": str(path), "missing": True}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:  # pragma: no cover - defensive CLI path
        return {"status": "FAIL", "path": str(path), "error_type": type(exc).__name__, "error": str(exc)}
    if not isinstance(data, dict):
        return {"status": "FAIL", "path": str(path), "error": "json_root_not_object"}
    data.setdefault("path", str(path))
    return data


def cleanup_status(report: dict[str, Any]) -> str:
    if report.get("cleanup_status") is not None:
        return norm(report.get("cleanup_status"))
    cleanup = report.get("cleanup")
    if isinstance(cleanup, dict):
        return norm(cleanup.get("status"))
    return "NOT_RUN"


def journey_pass_ids(report: dict[str, Any]) -> list[str]:
    journeys = report.get("journeys") or []
    if not isinstance(journeys, list):
        return []
    return [str(j.get("id")) for j in journeys if isinstance(j, dict) and norm(j.get("status")) == "PASS"]


def classify_pass_with_notes(name: str, report: dict[str, Any]) -> list[dict[str, str]]:
    """Classify why a PASS_WITH_NOTES/partial report is not a full semantic PASS."""
    reasons: list[dict[str, str]] = []
    status = norm(report.get("status"))
    mode = str(report.get("mode") or "")
    prod = norm(report.get("prod_live_smoke"))
    cstatus = cleanup_status(report)

    if status == "PASS_WITH_NOTES":
        if prod in PROD_NOT_RUN_STATUSES:
            reasons.append({"source": name, "reason": "prod_not_run", "detail": f"prod_live_smoke={prod}"})
        if mode in {"dry-run", "read-only-live"} or (name in {"business_journey", "api_contract"} and mode != "local_harness_execute"):
            reasons.append({"source": name, "reason": "local_dev_not_run", "detail": f"mode={mode or 'UNKNOWN'}"})
        if cstatus in {"NOT_RUN", "UNKNOWN"}:
            reasons.append({"source": name, "reason": "cleanup_not_run", "detail": f"cleanup_status={cstatus}"})
        if name == "openapi_drift" and status in {"PASS_WITH_NOTES", "NOT_RUN"}:
            reasons.append({"source": name, "reason": "openapi_drift_not_run", "detail": f"status={status}"})
        if name == "frontend_consumer" and status in {"PASS_WITH_NOTES", "NOT_RUN"}:
            reasons.append({"source": name, "reason": "frontend_consumer_not_run", "detail": f"status={status}"})
        if not reasons:
            reasons.append({"source": name, "reason": "unclassified_pass_with_notes", "detail": "status=PASS_WITH_NOTES without a known note marker"})
    elif status == "NOT_RUN":
        if name == "openapi_drift":
            reasons.append({"source": name, "reason": "openapi_drift_not_run", "detail": "report missing or NOT_RUN"})
        elif name == "frontend_consumer":
            reasons.append({"source": name, "reason": "frontend_consumer_not_run", "detail": "report missing or NOT_RUN"})
        elif name in {"business_journey", "api_contract"}:
            reasons.append({"source": name, "reason": "local_dev_not_run", "detail": "report missing or NOT_RUN"})
    return reasons


def validate_business(report: dict[str, Any]) -> list[dict[str, Any]]:
    failures: list[dict[str, Any]] = []
    required = {"auth-session-access", "product-asset-prompt", "wallet-commercial-billing"}
    passed = set(journey_pass_ids(report))
    if norm(report.get("status")) != "PASS":
        failures.append({"gate": "business_journey", "reason": "status_not_pass", "status": report.get("status")})
    if report.get("mode") != "local_harness_execute" or report.get("env") != "local":
        failures.append({"gate": "business_journey", "reason": "local_harness_execute_required", "mode": report.get("mode"), "env": report.get("env")})
    if cleanup_status(report) != "PASS":
        failures.append({"gate": "business_journey", "reason": "cleanup_status_not_pass", "cleanup_status": cleanup_status(report)})
    if not required.issubset(passed):
        failures.append({"gate": "business_journey", "reason": "required_journeys_missing", "required": sorted(required), "passed": sorted(passed & required)})
    if norm(report.get("prod_live_smoke")) not in PROD_NOT_RUN_STATUSES:
        failures.append({"gate": "business_journey", "reason": "prod_live_smoke_not_explicitly_not_run_or_blocked", "prod_live_smoke": report.get("prod_live_smoke")})
    return failures


def validate_api_contract(report: dict[str, Any]) -> list[dict[str, Any]]:
    failures: list[dict[str, Any]] = []
    if norm(report.get("status")) != "PASS":
        failures.append({"gate": "api_contract", "reason": "status_not_pass", "status": report.get("status")})
    if report.get("mode") != "local_harness_execute" or report.get("env") != "local":
        failures.append({"gate": "api_contract", "reason": "local_harness_execute_required", "mode": report.get("mode"), "env": report.get("env")})
    if cleanup_status(report) != "PASS":
        failures.append({"gate": "api_contract", "reason": "cleanup_status_not_pass", "cleanup_status": cleanup_status(report)})
    if norm((report.get("live_contract_evidence") or {}).get("status")) != "PASS":
        failures.append({"gate": "api_contract", "reason": "live_contract_evidence_not_pass", "live_contract_status": (report.get("live_contract_evidence") or {}).get("status")})
    if norm(report.get("prod_live_smoke")) not in PROD_NOT_RUN_STATUSES:
        failures.append({"gate": "api_contract", "reason": "prod_live_smoke_not_explicitly_not_run_or_blocked", "prod_live_smoke": report.get("prod_live_smoke")})
    return failures


def validate_contract_report(name: str, report: dict[str, Any], allowed: set[str] | None = None) -> list[dict[str, Any]]:
    allowed = allowed or {"PASS"}
    if norm(report.get("status")) in allowed:
        return []
    return [{"gate": name, "reason": "status_not_allowed", "status": report.get("status"), "allowed": sorted(allowed)}]


def validate_platform_matrix(report: dict[str, Any]) -> list[dict[str, Any]]:
    failures: list[dict[str, Any]] = []
    if norm(report.get("status")) != "PASS":
        failures.append({"gate": "platform_matrix", "reason": "status_not_pass", "status": report.get("status")})
    if norm(report.get("decision")) != "CAN_DEPLOY":
        failures.append({"gate": "platform_matrix", "reason": "decision_not_can_deploy", "decision": report.get("decision")})
    return failures


def main() -> int:
    reports = {
        "business_journey": load_json(BUSINESS_REPORT),
        "api_contract": load_json(API_CONTRACT_REPORT),
        "openapi_drift": load_json(OPENAPI_DRIFT_REPORT),
        "frontend_consumer": load_json(FRONTEND_CONSUMER_REPORT),
        "platform_matrix": load_json(PLATFORM_MATRIX_REPORT),
    }

    failures: list[dict[str, Any]] = []
    failures.extend(validate_business(reports["business_journey"]))
    failures.extend(validate_api_contract(reports["api_contract"]))
    failures.extend(validate_contract_report("openapi_drift", reports["openapi_drift"], {"PASS"}))
    failures.extend(validate_contract_report("frontend_consumer", reports["frontend_consumer"], {"PASS"}))
    failures.extend(validate_platform_matrix(reports["platform_matrix"]))

    pass_with_notes_reasons: list[dict[str, str]] = []
    for name, report in reports.items():
        pass_with_notes_reasons.extend(classify_pass_with_notes(name, report))

    # Prod live smoke is intentionally not run by local/dev release-quality-gate, but
    # the omission must be explicit and visible in both local execute reports.
    prod_values = {name: norm(reports[name].get("prod_live_smoke")) for name in ("business_journey", "api_contract")}
    for name, prod in prod_values.items():
        if prod in PROD_NOT_RUN_STATUSES and not any(r["source"] == name and r["reason"] == "prod_not_run" for r in pass_with_notes_reasons):
            pass_with_notes_reasons.append({"source": name, "reason": "prod_not_run", "detail": f"prod_live_smoke={prod}"})

    non_prod_note_reasons = {"local_dev_not_run", "cleanup_not_run", "openapi_drift_not_run", "frontend_consumer_not_run", "unclassified_pass_with_notes"}
    blocking_note_reasons = [r for r in pass_with_notes_reasons if r.get("reason") in non_prod_note_reasons]
    if blocking_note_reasons:
        failures.append({"gate": "semantic_notes", "reason": "blocking_pass_with_notes_reason", "notes": blocking_note_reasons})

    status = "FAIL" if failures else ("PASS_WITH_NOTES" if pass_with_notes_reasons else "PASS")
    decision = "CANNOT_DEPLOY" if failures else "CAN_DEPLOY"
    payload = {
        "generated_at_unix": int(time.time()),
        "status": status,
        "decision": decision,
        "policy": "Release-quality semantic validation requires local isolated business/API execute PASS with cleanup, OpenAPI drift PASS, frontend consumer PASS, and platform matrix PASS/CAN_DEPLOY. Prod live smoke may remain NOT_RUN only when explicit.",
        "reports": {name: {"path": report.get("path"), "status": report.get("status"), "mode": report.get("mode"), "env": report.get("env"), "cleanup_status": cleanup_status(report), "decision": report.get("decision"), "prod_live_smoke": report.get("prod_live_smoke")} for name, report in reports.items()},
        "business_journeys_passed": journey_pass_ids(reports["business_journey"]),
        "pass_with_notes_reasons": pass_with_notes_reasons,
        "failures": failures,
    }
    REPORT_DIR.mkdir(parents=True, exist_ok=True)
    REPORT_PATH.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"status": status, "decision": decision, "report": str(REPORT_PATH), "pass_with_notes_reasons": pass_with_notes_reasons, "failure_count": len(failures)}, ensure_ascii=False))
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
