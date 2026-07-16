// Package ledger is the backend's source of truth for reads, keyed by
// vault public key: the vault-to-Reap identity mapping (user, account, card)
// and the transaction cache mirrored from webhook events.
//
// It never computes balances; the spendable balance is owned by the issuer
// and only cached here.
package ledger
