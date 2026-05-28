# V Observability Event Specification

This document defines the first shared observability contract for V Platform and Agent Ecommerce. It is intentionally small and stable so teams do not add ad-hoc logs that cannot be queried later.

## Event naming

Use dot-separated, domain-first names:

```text
<product_or_platform>.<module>.<object_or_step>.<operation>.<phase>
```

Allowed phases for lifecycle events:

- `started`
- `finished`
- `failed`

Seed events in this slice:

```text
ecommerce.product_center.products.list.started
ecommerce.product_center.products.list.finished
ecommerce.product_center.products.list.failed
ecommerce.product_center.product.detail.started
ecommerce.product_center.product.detail.finished
ecommerce.product_center.product.detail.failed
ecommerce.product_center.product.create.started
ecommerce.product_center.product.create.finished
ecommerce.product_center.product.create.failed
ecommerce.product_center.product.update.started
ecommerce.product_center.product.update.finished
ecommerce.product_center.product.update.failed

ecommerce.visual_workflow.session.create.started
ecommerce.visual_workflow.session.create.finished
ecommerce.visual_workflow.session.create.failed
ecommerce.visual_workflow.source_reference.create.started
ecommerce.visual_workflow.source_reference.create.finished
ecommerce.visual_workflow.source_reference.create.failed
ecommerce.visual_workflow.deconstruction_job.create.started
ecommerce.visual_workflow.deconstruction_job.create.finished
ecommerce.visual_workflow.deconstruction_job.create.failed
ecommerce.visual_workflow.element.confirm.started
ecommerce.visual_workflow.element.confirm.finished
ecommerce.visual_workflow.element.confirm.failed
ecommerce.visual_workflow.generation_version.create.started
ecommerce.visual_workflow.generation_version.create.finished
ecommerce.visual_workflow.generation_version.create.failed
ecommerce.visual_workflow.asset.writeback.started
ecommerce.visual_workflow.asset.writeback.finished
ecommerce.visual_workflow.asset.writeback.failed

ecommerce.runtime.job.create.started
ecommerce.runtime.job.create.finished
ecommerce.runtime.job.create.failed
ecommerce.runtime.charge.reserve.started
ecommerce.runtime.charge.reserve.finished
ecommerce.runtime.charge.reserve.failed
ecommerce.runtime.charge_session.create.started
ecommerce.runtime.charge_session.create.finished
ecommerce.runtime.charge_session.create.failed
ecommerce.runtime.settlement.finalize.started
ecommerce.runtime.settlement.finalize.finished
ecommerce.runtime.settlement.finalize.failed
```

## Span naming

Span names omit the terminal phase and should match the operation:

```text
ecommerce.product_center.products.list
ecommerce.product_center.product.detail
ecommerce.product_center.product.create
ecommerce.product_center.product.update
ecommerce.visual_workflow.session.create
ecommerce.visual_workflow.source_reference.create
ecommerce.visual_workflow.deconstruction_job.create
ecommerce.visual_workflow.element.confirm
ecommerce.visual_workflow.generation_version.create
ecommerce.visual_workflow.asset.writeback
platform.diagnostics.request.summary
```

## Standard fields

Every structured event should use these fields where applicable:

```text
request_id
trace_id
service
module
operation
product_id
sku_code
session_id
job_id
provider
status
latency_ms
error_code
```

Additional low-cardinality IDs are allowed when they directly support troubleshooting, for example `runtime_job_id`, `reservation_id`, `charge_session_id`, `source_reference_id`, `version_id`, `asset_id`.

## Error fields

- `status`: `failed`
- `error_code`: stable machine code, not a raw provider message
- `error`: short sanitized message, max 300 characters

Do not use stack traces or raw upstream response bodies in standard request events.

## Sensitive fields forbidden in logs/spans

Do not record:

```text
token
secret
raw prompt or large prompt text
image original URL / signed URL / storage key
user private free text
provider key
full provider payload
full runtime manifest
full asset manifest
idempotency key raw value
```

Helpers in `internal/observability` redact obvious forbidden field names, but callers must still avoid passing sensitive payloads.

## request_id / trace_id inheritance

1. Incoming HTTP request:
   - accept `X-Request-ID` if present; otherwise generate one.
   - accept `X-Trace-ID` if present; otherwise use active OTel trace ID or fall back to `request_id`.
   - return both response headers.
2. Product → Platform internal calls:
   - current first slice logs typed client calls with local request context unavailable.
   - follow-up work should add context-aware platform client methods so `X-Request-ID` / `X-Trace-ID` can propagate from the incoming request.
3. Diagnostics:
   - use `request_id` to query logs first.
   - derive `trace_id` from matching logs if the caller did not provide one.
   - query Tempo/trace backend only after a trace ID exists.

## Implementation entrypoints

- Ecommerce helper: `ecommerce-backend/internal/observability`
- Platform helper: `platform-backend/internal/observability`
- Request diagnostics API: `GET /api/v1/audit/diagnostics/requests/:requestID`

## Query-side rule

Audit DB remains for immutable business facts only. Raw request logs stay in stdout/log backend. The diagnostics API may summarize Loki/Tempo results, but must not persist raw log lines into Postgres.
