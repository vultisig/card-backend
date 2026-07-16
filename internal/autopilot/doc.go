// Package autopilot watches card balances via webhook events and owns the
// refill trigger: low-balance detection, notification, and (later) handing
// the trigger to the plugin system that executes auto-top-ups within a
// user-granted policy. It never signs anything.
//
// Refill destinations are locked to the vault's own card deposit address,
// whether the transaction is user-signed or plugin-executed.
package autopilot
