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

// Client sends DogStatsD metrics over UDP. A nil *Client is valid and every
// method on it is a no-op, so callers can pass around a possibly-unconfigured
// client without nil checks at every call site.
type Client struct {
	conn   net.Conn
	prefix string
}

// New dials addr (host:port) for UDP. Dialing UDP never contacts the remote
// end, so this only fails on a malformed address — whether anything is
// actually listening at addr is never checked here or on any later send.
func New(addr, prefix string) (*Client, error) {
	if addr == "" {
		return nil, nil
	}
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, prefix: prefix}, nil
}

// Count sends a counter metric.
func (c *Client) Count(name string, value int64, tags ...string) {
	c.send(name, strconv.FormatInt(value, 10), "c", tags)
}

// Timing sends a duration as a DogStatsD timer (milliseconds).
func (c *Client) Timing(name string, d time.Duration, tags ...string) {
	c.send(name, strconv.FormatInt(d.Milliseconds(), 10), "ms", tags)
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
	_, _ = c.conn.Write([]byte(b.String()))
}
