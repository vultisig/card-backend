// Package statsd is a minimal DogStatsD (StatsD + tags) UDP client. It's a
// deliberately small hand-rolled client rather than a dependency: DogStatsD
// is a one-line-per-metric UDP text protocol, and metrics are always
// best-effort (dropped silently if nothing's listening), so there's nothing
// a library buys us here beyond what net.Dial gives for free.
package statsd

import (
	"net"
	"strconv"
	"strings"
	"time"
)

// dialTimeout bounds DNS resolution + connection setup in New. writeTimeout
// bounds each send: UDP writes normally return immediately (no handshake),
// but a stalled local network stack could otherwise block the request path
// indefinitely.
const (
	dialTimeout  = 2 * time.Second
	writeTimeout = 2 * time.Second
)

// Client sends DogStatsD metrics over UDP. A nil *Client is valid and every
// method on it is a no-op, so callers can pass around a possibly-unconfigured
// client without nil checks at every call site.
type Client struct {
	conn   net.Conn
	prefix string
}

// New dials addr (host:port) for UDP. Dialing UDP never contacts the remote
// end, so this only fails on a malformed address or (bounded by
// dialTimeout) a hostname that won't resolve — whether anything is actually
// listening at addr is never checked here or on any later send.
func New(addr, prefix string) (*Client, error) {
	if addr == "" {
		return nil, nil
	}
	conn, err := net.DialTimeout("udp", addr, dialTimeout)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, prefix: prefix}, nil
}

// Count sends a counter metric.
func (c *Client) Count(name string, value int64, tags ...string) {
	c.send(name, strconv.FormatInt(value, 10), "c", tags)
}

// Timing sends a duration as a DogStatsD timer (fractional milliseconds —
// DogStatsD's wire format accepts floats, and truncating to whole
// milliseconds would zero out any sub-millisecond duration).
func (c *Client) Timing(name string, d time.Duration, tags ...string) {
	ms := float64(d) / float64(time.Millisecond)
	c.send(name, strconv.FormatFloat(ms, 'f', -1, 64), "ms", tags)
}

func (c *Client) send(name, value, kind string, tags []string) {
	if c == nil {
		return
	}
	var b strings.Builder
	b.WriteString(c.prefix)
	b.WriteString(name)
	b.WriteByte(':')
	b.WriteString(value)
	b.WriteByte('|')
	b.WriteString(kind)
	if len(tags) > 0 {
		b.WriteString("|#")
		b.WriteString(strings.Join(tags, ","))
	}
	// Fire-and-forget: a dropped metric must never affect the request it
	// describes, so write errors (e.g. ECONNREFUSED on a loopback agent
	// that isn't running) are intentionally discarded, not logged per-call.
	// The write deadline bounds the call itself, not just the error.
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_, _ = c.conn.Write([]byte(b.String()))
}
