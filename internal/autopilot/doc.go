// Package autopilot watches card balances via webhook events and triggers
// vault-to-card refills within a user-approved policy (threshold, target,
// asset), executed through pre-signed batches or VultiServer co-signing.
//
// Refill destinations are locked to the vault's own card deposit address.
package autopilot
