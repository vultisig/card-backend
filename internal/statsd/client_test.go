package statsd

import (
	"net"
	"testing"
	"time"
)

func listen(t *testing.T) (*net.UDPConn, string) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, conn.LocalAddr().String()
}

func recv(t *testing.T, conn *net.UDPConn) string {
	t.Helper()
	buf := make([]byte, 512)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("no packet received: %v", err)
	}
	return string(buf[:n])
}

func TestClientCountAndTiming(t *testing.T) {
	conn, addr := listen(t)
	c, err := New(addr, "card_backend.")
	if err != nil {
		t.Fatal(err)
	}

	c.Count("http.request.count", 1, "method:GET", "status:200")
	if got, want := recv(t, conn), "card_backend.http.request.count:1|c|#method:GET,status:200"; got != want {
		t.Errorf("Count wire format = %q, want %q", got, want)
	}

	c.Timing("http.request.duration", 150*time.Millisecond, "route:/health")
	if got, want := recv(t, conn), "card_backend.http.request.duration:150|ms|#route:/health"; got != want {
		t.Errorf("Timing wire format = %q, want %q", got, want)
	}
}

func TestClientNoTagsOmitsHash(t *testing.T) {
	conn, addr := listen(t)
	c, err := New(addr, "")
	if err != nil {
		t.Fatal(err)
	}

	c.Count("foo", 3)
	if got, want := recv(t, conn), "foo:3|c"; got != want {
		t.Errorf("Count with no tags = %q, want %q", got, want)
	}
}

func TestNewEmptyAddrDisablesClient(t *testing.T) {
	c, err := New("", "card_backend.")
	if err != nil {
		t.Fatal(err)
	}
	if c != nil {
		t.Fatalf("expected nil client for empty addr, got %+v", c)
	}
}

// A nil *Client must be safe to call methods on — main.go relies on this to
// keep the server running when no statsd agent is configured.
func TestNilClientIsNoop(t *testing.T) {
	var c *Client
	c.Count("foo", 1, "tag:x")
	c.Timing("bar", time.Second)
}
