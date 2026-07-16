# 0002 — Issuer surface: Reap modular API, Consumer + USER_FUNDED

Status: ACCEPTED · 2026-07-16

## Context

Reap operates two API surfaces: a legacy card-issuing product and the modular API (docs.reap.global, version 2025-02-14). Program mode, account ownership, and funding model are fixed at issuer project creation and cannot be changed later.

## Decision

Build against the **modular API** with **Consumer** mode, **User** account ownership, **USER_FUNDED** funding.

## Consequences

- Cardholders deposit their own stablecoins to issuer-provisioned per-account wallets (production chains: Base, Polygon, Solana); the issuer authorizes every transaction against that balance. This preserves the self-custody-adjacent product story: no pooled Vultisig collateral, no Vultisig-held user funds.
- The service is never in the authorization hot path; spend control is exercised ahead of time via the issuer's spend-policy API (project/user/card scopes).
- Account creation registers the vault's own MPC keys as signers; the issuer's planned withdrawal mechanism is 2-of-2 (issuer + cardholder signer), so withdrawal rights belong to the vault, not to this backend. Until that ships, stranded-balance risk is mitigated by keeping refill targets small (Autopilot).
- External (real-time) authorization is issuer-side only available to program-funded projects — deliberately out of scope here.
