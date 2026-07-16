// Package policy compiles program tiers into issuer spend policies
// (limits, velocity, channel and merchant restrictions) and reconciles them
// whenever a cardholder's tier changes.
//
// Enforcement happens issuer-side at authorization time; this package only
// declares the rules ahead of time.
package policy
