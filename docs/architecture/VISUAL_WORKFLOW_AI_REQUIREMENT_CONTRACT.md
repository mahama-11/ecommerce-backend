# Visual Workflow V2 AI Requirement Contract

Status: active requirement contract for Ecommerce PR #5 (`feat/ecommerce-v2-visualworkflow-s1-s2`).

This document turns the V2 visual-commerce requirement into an engineering contract. It is not a UI brief. It defines how language-model planning, runtime execution, callbacks, result assets, and SKU writeback are assembled into the product requirement while preserving the Platform/Ecommerce boundary.

## Product requirement spine

The V2 backend must support this SKU-centric workflow:

```text
SKU/product facts
→ source references
→ visual deconstruction
→ attention decisions
→ intent planning
→ prompt planning
→ generation runtime
→ result asset projection
→ selected asset writeback
→ strategy/reporting summary
```

The user-facing requirement is an AI production line for SKU visuals, not a collection of independent AI buttons. Each step must leave a durable, product-scoped state that `stage-view` can render and that later steps can consume.

## Ownership boundary

### Platform owns shared execution truth

Platform is responsible for:

- provider configuration and API credentials;
- text/image runtime provider adapters such as `kimi_coding_text` and `minimax_text`;
- runtime capability/readiness;
- runtime job lifecycle, normalized output manifests, callbacks, storage registry, billing, metering, and charge sessions;
- provider fallback/routing such as Kimi-first with Minimax fallback when configured.

Ecommerce must not call Kimi/Minimax/provider APIs directly.

### Ecommerce owns product workflow truth

Ecommerce is responsible for:

- visual workflow sessions;
- source references and sanitized source facts;
- deconstruction jobs/elements and product-visible statuses;
- attention tree decisions;
- intent spec and prompt plan projections;
- generation versions and selected result projections;
- strategy report projection;
- SKU/Product asset writeback through product-owned asset relations.

Ecommerce stores only sanitized business projections and Platform-created references. It must not persist raw provider payloads or expose provider/storage/billing internals to frontend clients.

## Language-model integration requirement

Language model support is part of the requirement through Platform runtime task types, not a direct product-service dependency.

Required Platform task bindings:

```text
text_reasoning
intent_planning
prompt_planning
strategy_report
```

Expected provider routing:

```text
kimi_coding_text  -> preferred primary provider when configured and ready
minimax_text      -> fallback provider when configured and available
```

Provider credentials are deployment configuration/secret material. They must never be committed, embedded in examples, echoed in logs, or written into PR comments.

Ecommerce command endpoints create Platform runtime jobs for the relevant task type and then wait for trusted Platform callbacks/results. If Platform capability is missing, provider credentials are invalid, quota is exceeded, or Prompt Center snapshots are unavailable, Ecommerce must fail closed to `contract_needed` / `capability_unavailable` / `blocked`; it must not fabricate AI results or runtime IDs.

## Requirement-to-endpoint mapping

### Source facts

Requirement: user can start from an uploaded asset, product asset, URL, or later video frame.

Backend contract:

```text
POST /v2/visual-workflows/:session_id/source-references
GET  /v2/visual-workflows/:session_id/source-references
```

Engineering rule: URL resolution is bounded metadata probing only. Private/loopback hosts, oversized responses, unsupported schemes, login/anti-bot flows, and video frame extraction must return honest blockers rather than crawler-like claims.

### Deconstruction

Requirement: extract visual elements from source material and make them reviewable.

Backend contract:

```text
POST /v2/visual-workflows/:session_id/deconstruction-jobs
GET  /v2/visual-workflows/:session_id/deconstruction-elements
PATCH /v2/visual-workflows/:session_id/deconstruction-elements/:element_id
```

Engineering rule: runtime execution is created through Platform. Trusted callback ingestion updates product-visible status and sanitized elements only.

### Attention decisions

Requirement: support keep/replace/drop/review decisions before prompt planning.

Backend contract:

```text
POST /v2/visual-workflows/:session_id/attention-tree
```

Allowed decisions:

```text
keep
replace
drop
needs_review
```

Engineering rule: confidence must be bounded in `[0,1]`. Unknown decisions must be rejected. Client-supplied runtime/provider/storage artifacts are forbidden.

### Intent planning

Requirement: assemble source facts and selected elements into structured SKU visual intent.

Backend contract:

```text
POST /v2/visual-workflows/:session_id/intent-planner-jobs
```

Runtime requirement:

```text
task_type=intent_planning
provider selected by Platform runtime routing
trusted callback writes sanitized intent_spec
```

### Prompt planning

Requirement: convert visual intent and Prompt Center context into provider-executable prompt plan.

Backend contract:

```text
POST /v2/visual-workflows/:session_id/prompt-planner-jobs
```

Runtime requirement:

```text
task_type=prompt_planning
Prompt Center compiled prompt/source asset snapshot required
trusted callback writes sanitized prompt_plan
```

If prompt snapshot is missing or invalid, return `contract_needed` and do not create a fake runtime job.

### Image generation

Requirement: generate candidate visuals as traceable generation versions.

Backend contract:

```text
POST /v2/visual-workflows/:session_id/generation-versions
GET  /v2/visual-workflows/:session_id/generation-versions
```

Runtime requirement:

```text
Platform RuntimeInputManifest built from prompt plan + source asset snapshot
Platform normalized output manifest ingested through trusted callbacks
result assets projected onto generation versions
```

### Selection and SKU writeback

Requirement: selected AI result can become a SKU/Product asset.

Backend contract:

```text
POST /v2/visual-workflows/:session_id/generation-versions/:version_id/select
POST /v2/visual-workflows/:session_id/generation-versions/:version_id/writeback-selected-asset
```

Engineering rule: writeback uses product-owned asset relations/projections. Public responses must be sanitized and must not expose raw storage keys/provider metadata.

### Strategy report

Requirement: summarize source facts, intent, prompt, and generation state into lightweight commercial guidance.

Backend contract:

```text
POST /v2/visual-workflows/:session_id/strategy-report-jobs
```

Runtime requirement:

```text
task_type=strategy_report
trusted callback writes sanitized strategy report projection
```

This slice does not claim full autonomous pricing, competitor crawling, or sales proposal generation.

## Stage-view requirement

`stage-view` is the product read model and frontend integration source of truth:

```text
GET /v2/visual-workflows/:session_id/stage-view
```

It must aggregate sanitized workflow state:

```text
session
source_reference
deconstruction_job
deconstruction_elements
attention decisions
intent_spec
prompt_plan
generation_versions
strategy report projection when available
runtime_capabilities
readiness/blockers
```

Frontend clients must render from `stage-view` and issue command endpoints for actions. They must not infer provider state from Platform internals.

## Forbidden client fields

Public Ecommerce API requests must reject or ignore client attempts to submit:

```text
runtime_job_id
provider_job_id
image_job_id
storage_key
billing_* / charge_session_*
raw_provider_payload
raw_storage_metadata
api_key / token / secret
```

Persisted server-created references may exist internally, but they are never client-owned.

## State and error semantics

Use honest product states:

```text
ready
pending
processing
running
completed
failed
canceled
contract_needed
capability_unavailable
blocked
needs_review
invalid
not_found
conflict
```

Rules:

- unknown internal callback statuses are invalid contract drift;
- not-found jobs return not_found semantics;
- replay/conflicting terminal transitions return conflict or idempotent no-op only when explicitly safe;
- provider quota/auth/rate-limit problems must surface as blocked/contract-needed provider readiness, not as successful AI output.

## Engineering acceptance

A slice is not complete unless it has:

1. Platform/Ecommerce ownership boundary respected.
2. Server-created runtime references only.
3. Sanitized public DTOs and no raw provider/storage/billing leakage.
4. Fail-closed handling for missing capability, invalid config, quota/rate limit, and missing Prompt Center snapshot.
5. Idempotency key or replay-safe behavior for async job creation/callback handling.
6. Unit tests for happy path, blocked capability, invalid public input, malformed callback/result, and forbidden field handling.
7. Documentation updated in this file and `VISUAL_WORKFLOW_FRONTEND_CONTRACT.md` when endpoint semantics change.
8. PR evidence listing exact test commands and remaining live-provider blockers.

## Current known external blocker

Minimax live provider authentication/usage may be blocked by account quota/rate-limit. The correct engineering behavior is to route text tasks through Platform provider selection, prefer a ready provider such as Kimi when configured, and expose readiness blockers when no provider is available. Do not bypass Platform or hardcode provider credentials in Ecommerce.
