# Agent Ecommerce Backend - Agent Context

> This repository owns Agent Ecommerce product workflows. Reuse platform auth, org, RBAC, billing, and entitlement capabilities through service contracts instead of duplicating their source-of-truth data.

## 1. Purpose

`v-ecommerce-backend` is the product backend for Agent Ecommerce.

It should host:

- product-facing orchestration for AI commerce workflows
- template marketplace and saved-template product contracts
- workflow state, asset linkage, and delivery linkage owned by the product
- future product-owned job orchestration, asset pipelines, and reporting semantics
- backend infrastructure baseline for metrics, tracing, logs, request context, and internal service auth

It should not host:

- shared login/register truth
- shared organization/membership truth
- shared RBAC truth
- shared subscription, wallet, payment, or metering truth

## 2. Key Documents

- [Backend Guide](docs/BACKEND_GUIDE.md)
- [Service Boundary](docs/architecture/SERVICE_BOUNDARY.md)
- [Image Generation Runtime Integration](docs/architecture/IMAGE_GENERATION_RUNTIME_INTEGRATION.md)
- [Visual Workflow V2 Frontend Contract](docs/architecture/VISUAL_WORKFLOW_FRONTEND_CONTRACT.md)
- [OpenAPI Guide](docs/openapi/README.md)
- [Template Center Design](../docs/architecture/AGENT_ECOMMERCE_TEMPLATE_CENTER_DESIGN.md)
- [Template Center Evolution Plan](../docs/architecture/AGENT_ECOMMERCE_TEMPLATE_CENTER_EVOLUTION_PLAN.md)
- [Template Center Data Model](docs/architecture/TEMPLATE_CENTER_DATA_MODEL.md)

## 3. Commands

```bash
cd v-ecommerce-backend
go mod tidy
go test ./...
go run ./cmd/server -config config.local
make run
make test
```

## 4. Documentation Rules

- Add backend docs under `docs/` or `docs/architecture/`
- Keep route, boundary, and migration docs aligned with code
- Update this file and the root `AGENTS.md` whenever long-lived backend docs are added
