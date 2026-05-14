# Visual Workflow V2 Frontend Contract

Status: active draft for Ecommerce PR #5 (`feat/ecommerce-v2-visualworkflow-s1-s2`).

This document is the frontend handoff contract for the V2 SKU visual workflow. It should be read together with `VISUAL_WORKFLOW_AI_REQUIREMENT_CONTRACT.md`, which maps the language-model/runtime work back to the product requirement. Frontend should consume the product backend only; it must not call Platform runtime/provider APIs or carry provider/runtime/storage/billing internals.

## Contract principles

1. `stage-view` is the read model and UI source of truth.
2. User actions call small command endpoints, then refetch `stage-view`.
3. Runtime/provider IDs are server-created only. Frontend must never submit `runtime_job_id`, `provider_job_id`, `storage_key`, `billing_*`, raw provider payload, or API keys.
4. Unavailable capability is represented as `contract_needed`, `blocked`, or `capability_unavailable` in `readiness.blockers` / command `blockers`. Frontend should show an honest disabled state and reason, not fake success.
5. Async AI jobs are fire-and-refresh: command response gives `runtime_job_id/status/progress`, but rendered state comes from subsequent `GET stage-view` polling or refresh.

## Base path and auth

All routes are protected Ecommerce backend routes under the existing frontend API client/base path.

```text
GET  /v2/visual-workflows/:session_id/stage-view
```

Prefer using the project service layer, for example:

```ts
visualWorkflowService.getStageView(sessionId)
```

Do not scatter raw `fetch` in React pages.

## Main frontend flow

```text
1. Create/load session
2. Add source reference
3. Create deconstruction job
4. Review deconstruction elements
5. Apply attention tree decisions
6. Start intent planner
7. Start prompt planner
8. Create generation version / generation runtime
9. Select/write back result asset
10. Optional strategy report
```

Frontend should implement this as a state machine over `StageView.current_stage`, `status`, `readiness`, and command responses.

## Routes

### Session / read model

```text
POST /products/:product_id/v2/visual-sessions
POST /v2/visual-workflows/sessions
GET  /v2/visual-workflows/sessions
GET  /v2/visual-workflows/:session_id
PATCH /v2/visual-workflows/:session_id
POST /v2/visual-workflows/:session_id/cancel
GET  /v2/visual-workflows/:session_id/stage-view
```

### Source / deconstruction

```text
POST  /v2/visual-workflows/:session_id/source-references
GET   /v2/visual-workflows/:session_id/source-references
PATCH /v2/visual-workflows/:session_id/source-references/:source_reference_id
POST  /v2/visual-workflows/:session_id/deconstruction-jobs
GET   /v2/visual-workflows/:session_id/deconstruction-jobs/:job_id
GET   /v2/visual-workflows/:session_id/deconstruction-elements
PATCH /v2/visual-workflows/:session_id/deconstruction-elements/:element_id
POST  /v2/visual-workflows/:session_id/deconstruction-elements:confirm
```

### AI planning / strategy

```text
POST /v2/visual-workflows/:session_id/attention-tree
POST /v2/visual-workflows/:session_id/intent-planner-jobs
POST /v2/visual-workflows/:session_id/prompt-planner-jobs
POST /v2/visual-workflows/:session_id/strategy-report-jobs
```

### Generation / writeback

```text
POST  /v2/visual-workflows/:session_id/generation-versions
GET   /v2/visual-workflows/:session_id/generation-versions
GET   /v2/visual-workflows/:session_id/generation-versions/:version_id
PATCH /v2/visual-workflows/:session_id/generation-versions/:version_id
POST  /v2/visual-workflows/:session_id/generation-versions/:version_id/select
POST  /v2/visual-workflows/:session_id/generation-versions/:version_id/writeback-selected-asset
```

## StageView shape

```ts
export interface VisualWorkflowStageView {
  session_id: string
  product_id: string
  sku_code: string
  tool_slug?: string
  template_id?: string
  template_version_id?: string
  current_stage: string
  status: string
  readiness: Readiness
  source_reference?: SourceReference
  deconstruction_job?: DeconstructionJob
  deconstruction_elements: DeconstructionElement[]
  intent_spec: Record<string, unknown>
  prompt_plan: Record<string, unknown>
  generation_versions: GenerationVersion[]
  runtime_capabilities?: RuntimeCapability[]
  runtime_capability_error?: RuntimeCapabilityError
  updated_at: string
}
```

Recommended polling:

```text
- After a command returns pending/running/processing: poll stage-view every 2-3s.
- Stop polling on completed/failed/contract_needed/canceled.
- User-triggered refresh should always refetch stage-view.
```

## Command contracts

### Create source reference

```ts
POST /v2/visual-workflows/:session_id/source-references

{
  source_kind: 'upload' | 'product_asset' | 'url' | 'video_frame',
  source_ref?: string,
  source_url?: string,
  asset_id?: string,
  asset_relation_id?: string,
  mime_type?: string,
  metadata?: Record<string, unknown>
}
```

For `source_kind=url`, backend performs a bounded metadata resolver and returns sanitized `metadata.url_metadata` when successful. If blocked, show `status=contract_needed` / `error_message`.

### Apply attention tree

```ts
POST /v2/visual-workflows/:session_id/attention-tree

{
  decisions: [{
    element_id: string,
    decision: 'keep' | 'replace' | 'drop' | 'needs_review',
    group_path?: string[],
    target_asset_id?: string,
    rationale?: string,
    confidence?: number,
    metadata?: Record<string, unknown>
  }],
  drift_controls?: Record<string, unknown>
}
```

UI mapping:

```text
keep         -> selected/approved chip
replace      -> needs replacement / target asset optional
replace/drop -> exclude from next prompt unless user reverts
needs_review -> warning state, user action needed
```

### Start intent planner

```ts
POST /v2/visual-workflows/:session_id/intent-planner-jobs

{
  source_reference_id?: string,
  element_ids?: string[],
  marketplace?: string,
  locale?: string,
  drift_controls?: Record<string, unknown>,
  idempotency_key?: string,
  metadata?: Record<string, unknown>
}
```

### Start prompt planner

```ts
POST /v2/visual-workflows/:session_id/prompt-planner-jobs

{
  prompt_id?: string,
  template_id?: string,
  marketplace?: string,
  locale?: string,
  drift_controls?: Record<string, unknown>,
  prompt_variables?: Record<string, unknown>,
  idempotency_key?: string
}
```

### Start strategy report

```ts
POST /v2/visual-workflows/:session_id/strategy-report-jobs

{
  marketplace?: string,
  locale?: string,
  report_goal?: string,
  source_facts?: Record<string, unknown>,
  idempotency_key?: string
}
```

### Planner/strategy command response

```ts
interface PlannerJobResponse {
  session_id: string
  runtime_job_id?: string
  status: string
  stage: string
  progress: number
  blockers?: ReadinessBlocker[]
  idempotency_key?: string
}
```

If `runtime_job_id` is absent and `blockers` exists, render disabled/blocked state.

## Frontend error handling

Use these UI states consistently:

```text
ready        -> action enabled
processing   -> progress/polling
completed    -> show resulting stage-view data
contract_needed / capability_unavailable / blocked -> disabled action with reason
failed       -> retryable error panel if command is safe/idempotent
invalid      -> frontend bug or stale UI state; refresh and show safe error
```

Do not display internal/provider terms to customer-facing users. Use product language:

```text
"素材解析能力暂不可用"
"AI 规划服务暂不可用"
"请先选择至少一个视觉元素"
"结果生成中"
```

## QA expectations for frontend integration

Minimum frontend runtime QA after integration:

1. Create/load a visual workflow session.
2. Add URL source reference and verify `metadata.url_metadata` appears or honest blocker appears.
3. Apply one `keep` and one `needs_review` attention decision; refetch stage-view and assert persistence.
4. Trigger intent planner and prompt planner; verify command response and stage-view state.
5. Trigger strategy report; verify report appears in stage-view/session metadata when callback arrives, or blocked state is shown if provider unavailable.
6. Verify no browser request submits forbidden fields: `runtime_job_id`, `provider_job_id`, `storage_key`, `billing_*`, API keys.

Use stable `data-testid` selectors around workflow stages and primary actions. Typecheck/build is not enough; run a real browser/API roundtrip once the frontend is wired.
