APP_NAME=ecommerce-service
CONFIG?=config.local

.PHONY: tidy test test-quick run build guardrails coverage-gate route-inventory-gate platform-contract-gate openapi-drift-gate frontend-consumer-sweep platform-contract-matrix contract-gate evidence-semantic-validator commercial-wallet-gate infra-crosscut-gate observability-gate security-redaction-gate quality-gate pr-quality-gate core-business-pr-gate release-quality-gate

tidy:
	go mod tidy

test:
	go test ./...

test-quick:
	./scripts/test-quick.sh

guardrails:
	./scripts/check-guardrails.sh

coverage-gate:
	./scripts/coverage-gate.sh

route-inventory-gate:
	./scripts/route-inventory-gate.sh

platform-contract-gate:
	./scripts/platform-contract-gate.sh

openapi-drift-gate:
	./scripts/openapi-drift-gate.sh

frontend-consumer-sweep:
	./scripts/frontend-consumer-sweep.sh

platform-contract-matrix:
	./scripts/platform-contract-matrix.sh

contract-gate: openapi-drift-gate frontend-consumer-sweep platform-contract-matrix

evidence-semantic-validator:
	./scripts/evidence-semantic-validator.py

commercial-wallet-gate:
	./scripts/commercial-wallet-gate.sh

infra-crosscut-gate:
	./scripts/infra-crosscut-gate.sh

observability-gate:
	./scripts/observability-gate.sh

security-redaction-gate:
	./scripts/security-redaction-gate.sh

quality-gate: guardrails test-quick coverage-gate route-inventory-gate platform-contract-gate commercial-wallet-gate infra-crosscut-gate observability-gate security-redaction-gate

pr-quality-gate: test quality-gate

core-business-pr-gate: release-quality-gate

release-quality-gate: quality-gate openapi-drift-gate frontend-consumer-sweep platform-contract-gate
	./scripts/ecommerce-backend-critical-journey-smoke.py --env local --execute --fixture isolated --cleanup
	./scripts/ecommerce-backend-api-contract-smoke.py --env local --execute --fixture isolated --cleanup
	./scripts/platform-contract-matrix.sh
	./scripts/evidence-semantic-validator.py

run:
	go run ./cmd/server -config $(CONFIG)

build:
	go build -o bin/$(APP_NAME) ./cmd/server
