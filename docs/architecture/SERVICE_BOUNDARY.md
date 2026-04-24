# Agent Ecommerce Service Boundary

## 1. Principle

Business infrastructure can be platformized.
Business workflows remain product-owned.

## 2. Platform Reuse

Agent Ecommerce should consume shared capabilities from `v-platform-backend` for:

- auth and identity
- org and membership context
- roles and permissions
- entitlement and subscription truth
- wallet, coupon, reward, and metering truth

## 3. Product Ownership

Agent Ecommerce backend should own:

- product-shaped frontend APIs
- template-market and saved-template experience contracts
- workflow state aggregation across chat, design, delivery, and asset modules
- future AI workflow orchestration and product reporting semantics
- product-local preference and activity persistence

## 4. First Migration Slice

The first migration slice intentionally targets cross-page state that is currently backed by frontend local storage:

- template saves
- workflow feed events
- linked design assets
- linked deliveries
- template bridges

This lets the frontend remove the most coupled mock state first without waiting for full auth, billing, or job systems.

## 5. Shared Auth and Billing Projection

Agent Ecommerce backend should:

- call platform public auth APIs for register and login
- validate platform-issued JWTs for product session and access APIs
- call platform internal APIs for user profile, org access context, and wallet summary
- project those shared truths into product-facing response shapes required by the frontend

Agent Ecommerce backend should not:

- create a second source of truth for passwords, access tokens, or memberships
- fork platform wallet balances into product-owned billing truth
