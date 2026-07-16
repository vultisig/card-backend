// Package device owns device enrollment and trust: it verifies the MPC
// threshold signature that binds a device to a vault public key, issues and
// revokes device tokens, and enforces step-up verification for boundary
// changes (new device, card re-bind, autopilot source change).
//
// It is the only package that decides whether a request is authenticated.
package device
