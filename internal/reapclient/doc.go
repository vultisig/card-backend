// Package reapclient is the typed HTTP client for the Reap modular API
// (docs.reap.global). It owns the API key, version header, idempotency keys,
// and rate-limit handling.
//
// No other package may call Reap directly; swapping API generations must be
// a change local to this package.
package reapclient
