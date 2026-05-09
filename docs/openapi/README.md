# Agent Ecommerce OpenAPI

## Current Scope

The first Swagger / OpenAPI scope focuses on frontend integration for the current backend baseline:

- `POST /api/v1/ecommerce/auth/register`
- `POST /api/v1/ecommerce/auth/login`
- `GET /api/v1/ecommerce/auth/session`
- `GET /api/v1/ecommerce/access/me`
- `GET /api/v1/ecommerce/templates/saved`
- `POST /api/v1/ecommerce/templates/saved`
- `GET /api/v1/ecommerce/workflow/events`
- `POST /api/v1/ecommerce/workflow/events`
- `GET /api/v1/ecommerce/workflow/template-bridges`
- `POST /api/v1/ecommerce/workflow/template-bridges`
- `GET /api/v1/ecommerce/assets/linked-designs`
- `POST /api/v1/ecommerce/assets/linked-designs`
- `GET /api/v1/ecommerce/assets/library`
- `GET /api/v1/ecommerce/assets/library/stats`
- `PATCH /api/v1/ecommerce/assets/library/:relationId/governance`
- `GET /api/v1/ecommerce/deliveries/linked`
- `POST /api/v1/ecommerce/deliveries/linked`

## Generate OpenAPI

Install `swag` first:

```bash
go install github.com/swaggo/swag/cmd/swag@latest
```

Then run:

```bash
./scripts/gen-swagger.sh
```

Generated output will be written to:

```bash
docs/openapi/
```

## Notes

- Agent Ecommerce backend owns the frontend-facing product API contract.
- Shared identity, org, and wallet truth still live in `v-platform-backend`.
- API responses include request-scoped metadata such as `request_id`, and the service also emits `X-Request-ID` / `X-Trace-ID` headers for correlation.
- Current OpenAPI scope intentionally covers the stable auth/access/workspace baseline first, before designer/jobs/catalog domains are added.
