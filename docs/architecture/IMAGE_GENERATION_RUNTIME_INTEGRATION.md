# Agent Ecommerce Image Generation Runtime Integration

Owner: `v-ecommerce-backend` product backend, with `v-platform-backend` runtime as the shared execution capability.

## 1. Purpose

This document defines the correct integration model when Agent Ecommerce wants to use shared platform image-generation runtime capabilities such as `ComfyUI Bridge`, `Volcengine`, or future provider chains.

It is intentionally written from the product-backend point of view:

- what `v-ecommerce-backend` owns
- what `v-platform-backend` owns
- which contracts must stay stable
- how product jobs, assets, callbacks, and route preferences should be modeled

This document does **not** turn Agent Ecommerce into a provider-specific adapter. Product code should integrate with platform runtime contracts, not with raw third-party provider APIs.

## 2. Boundary Decision

### 2.1 Platform Owns

- provider adapters such as `comfyui_bridge` and `volcengine`
- provider submit / poll / callback reconciliation
- provider fallback chains and retry classification
- runtime job execution state machine
- result normalization and platform storage persistence
- shared runtime routing policy (`objective`, preferred providers, fallback rules)

### 2.2 Agent Ecommerce Owns

- product job semantics such as "product hero image", "bundle series", or marketplace-specific variants
- product-facing APIs, views, filters, and admin semantics
- product-owned job table and product-owned asset linkage
- product-facing asset content routes
- product-facing state projection from platform runtime into stable ecommerce job states

### 2.3 What Must Not Happen

- `v-ecommerce-backend` must not call `ComfyUI Bridge` directly
- `v-ecommerce-backend` must not persist raw provider task payloads as product truth
- frontend must not consume provider result URLs directly
- frontend must not assume platform public `/storage/*` access exists

## 3. Product Data Ownership

Agent Ecommerce should keep its own product domain tables and treat platform runtime as an execution subsystem, not as the owner of product business records.

Recommended minimum tables:

### 3.1 Product Job Table

Suggested table: `ecommerce_image_jobs`

Recommended fields:

- `id`
- `organization_id`
- `user_id`
- `scene_type`
- `input_mode`
- `source_asset_id`
- `runtime_job_id`
- `status`
- `stage`
- `stage_message`
- `selected_result_asset_id`
- `metadata`
- `created_at`
- `updated_at`

### 3.2 Product Asset Table

Suggested table: `ecommerce_assets`

Recommended fields:

- `id`
- `organization_id`
- `asset_type`
- `storage_key`
- `mime_type`
- `width`
- `height`
- `file_name`
- `metadata`
- `created_at`
- `updated_at`

The product asset record is the ecommerce truth. Platform storage is the shared media capability behind it.

## 4. Source Asset Flow

For `image_to_image`, the correct flow is:

1. frontend uploads image to `v-ecommerce-backend`
2. `v-ecommerce-backend` uploads that media to platform storage
3. platform returns `storage_key`
4. ecommerce stores `storage_key` in its own asset record
5. ecommerce creates a runtime job referencing that `storage_key`

Do **not** create runtime jobs from:

- temporary browser blob URLs
- third-party provider URLs
- public platform static URLs

The shared runtime now expects source assets to be resolvable through platform storage, so the product backend should persist and forward `storage_key` as the canonical source-asset reference.

## 5. Runtime Create Contract

Agent Ecommerce should create runtime jobs through platform internal runtime APIs. The product backend should describe business intent and input manifests, while platform decides the provider route.

Recommended request shape:

```json
{
  "product_code": "ecommerce",
  "task_type": "image_generation",
  "provider_mode": "async",
  "organization_id": "org_xxx",
  "user_id": "user_xxx",
  "source_type": "ecommerce_image_job",
  "source_id": "job_xxx",
  "idempotency_key": "ecommerce:job_xxx:create_runtime",
  "input_manifest": "{\"input_mode\":\"image_to_image\",\"params_snapshot\":{\"prompt\":\"premium ecommerce hero image\",\"negative_prompt\":\"blur, distortion\",\"steps\":8,\"cfg\":1.0,\"denoise\":0.7,\"width\":1024,\"height\":1024},\"source_asset_ids\":[\"asset_src_xxx\"],\"source_assets\":[{\"id\":\"asset_src_xxx\",\"storage_key\":\"ecommerce/assets/source-1.png\",\"mime_type\":\"image/png\",\"width\":1024,\"height\":1024}],\"requested_variants\":1}",
  "route_snapshot": "{\"objective\":\"quality\",\"preferred_providers\":[\"comfyui_bridge\",\"volcengine\"]}",
  "metadata": "{\"scene_type\":\"product_main_image\",\"template_code\":\"P1-T01\"}",
  "priority": 100,
  "max_attempts": 3,
  "timeout_seconds": 600
}
```

Key points:

- `task_type` should stay product-agnostic and platform-recognizable
- `idempotency_key` is mandatory to prevent duplicate runtime jobs
- `source_type` and `source_id` should point back to ecommerce product records
- `metadata` should describe product semantics, not provider implementation details

## 6. Route Preferences and Fallback

Product code may express business preference, but should avoid hardcoding raw provider branching.

### 6.1 Preferred Normal Path

Use `route_snapshot.objective` as the main routing hint:

- `quality`
- `speed`
- `cost`
- `balanced`

Typical mapping:

- product hero image: `quality`
- fast batch listing generation: `speed`
- large low-cost drafts: `cost`

### 6.2 Preferred Providers

`preferred_providers` may be passed when the product has a strong preference or rollout intent, for example:

```json
{
  "objective": "quality",
  "preferred_providers": ["comfyui_bridge", "volcengine"]
}
```

This is still a routing hint, not a requirement for product code to manually handle fallback.

### 6.3 Fallback Principle

Product code must not implement:

- "try provider A"
- "if A fails, retry provider B in product code"

That fallback belongs to platform runtime. Product code should only observe the normalized runtime result and product callback outcome.

## 7. Product Callback Contract

Before Agent Ecommerce can become the first real product consumer of the new async runtime path, platform callback handling must be generalized away from Menu-specific callback clients.

Agent Ecommerce should expose two internal callback surfaces:

### 7.1 Runtime Status Callback

```http
POST /internal/v1/ecommerce/jobs/:jobID/runtime
```

Suggested body:

```json
{
  "status": "processing",
  "stage": "provider_running",
  "stage_message": "ComfyUI Bridge processing",
  "progress": 25,
  "eta_seconds": 5,
  "provider_job_id": "task_xxx"
}
```

Responsibilities:

- update ecommerce product job status
- persist stage and stage message
- persist progress snapshot if shown to frontend
- link `provider_job_id` for diagnostics if desired

### 7.2 Runtime Result Callback

```http
POST /internal/v1/ecommerce/jobs/:jobID/results
```

Suggested body:

```json
{
  "status": "completed",
  "progress": 100,
  "stage_message": "ComfyUI Bridge generation completed",
  "metadata": {
    "provider": "comfyui_bridge",
    "task_id": "task_xxx"
  },
  "variants": [
    {
      "index": 0,
      "status": "ready",
      "is_selected": true,
      "asset": {
        "asset_type": "generated",
        "source_type": "generated",
        "storage_key": "ecommerce/assets/generated-1.png",
        "source_url": "",
        "preview_url": "",
        "mime_type": "image/png",
        "width": 1024,
        "height": 1024
      }
    }
  ]
}
```

Responsibilities:

- create ecommerce-owned generated asset records
- attach them to the product job
- mark the product job as `completed`
- select the primary result asset if the business flow requires it

## 8. Product Asset Access Contract

Ecommerce frontend should not access provider URLs or platform internal media paths directly.

Recommended access model:

- ecommerce backend persists product asset records
- ecommerce backend exposes product-facing asset content routes
- frontend uses ecommerce-owned URLs such as:
  - `/api/v1/ecommerce/assets/:assetID/content`

This keeps provider migration, storage relocation, and fallback behavior invisible to the frontend contract.

## 9. Failure Semantics

Product code should only depend on normalized states:

- `queued`
- `processing`
- `completed`
- `failed`
- `canceled`

Platform may internally use more granular runtime stages such as:

- `provider_accepted`
- `provider_running`
- `fallback_scheduled`

Those internal stages are useful for diagnostics, but product APIs should avoid leaking provider-specific noise unless the UI truly needs it.

Recommended product diagnostics fields:

- `runtime_job_id`
- `provider_job_id` (optional mirror)
- `last_error_code`
- `last_error_message`

## 10. First Rollout Recommendation

Do not start with the most complex multi-image batch scenario.

Recommended first real integration:

- scene: product hero image refinement
- mode: `image_to_image`
- source assets: single source image
- result variants: 1
- route objective: `quality`

Why:

- easiest to validate end-to-end
- exercises source storage flow
- exercises async runtime, poll, callback, and result persistence
- proves the product callback contract without requiring batch orchestration first

## 11. Current Platform Callback Expectation

Platform runtime now treats the final product callback hop as a product callback abstraction rather than a Menu-only shortcut. For Agent Ecommerce, the expected endpoint registration is:

- `callback_kind = ecommerce_internal`
- ecommerce runtime status callback
- ecommerce runtime results callback

That means ecommerce should implement the internal callback routes before enabling runtime jobs in real product flows:

- product job tables
- asset tables
- runtime create service
- internal callback handlers
- frontend-facing asset content routes

## 12. Implementation Checklist

- add ecommerce product job table
- add ecommerce product asset table
- add product runtime create service calling platform internal runtime API
- upload source images to platform storage first and persist `storage_key`
- include `route_snapshot.objective` in runtime create requests
- add `/internal/v1/ecommerce/jobs/:jobID/runtime`
- add `/internal/v1/ecommerce/jobs/:jobID/results`
- add `/api/v1/ecommerce/assets/:assetID/content`
- expose normalized job status to frontend
- validate one real `image_to_image` workflow before expanding to batch template scenarios
