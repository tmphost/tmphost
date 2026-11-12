// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"io"
	"net"
	"net/http"
	"net/netip"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// peerListener reports every accepted connection as coming from peer, so
// a test can play a Cloudflare edge (or a stranger) over a real socket.
type peerListener struct {
	net.Listener
	peer net.Addr
}

type peerConn struct {
	net.Conn
	peer net.Addr
}

func (l *peerListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &peerConn{Conn: c, peer: l.peer}, nil
}

func (c *peerConn) RemoteAddr() net.Addr { return c.peer }

// clientIP serves an app built with cfg from peer, sends one request with
// the given headers and returns the c.IP() the app saw.
func clientIP(t *testing.T, cfg AppConfig, peer string, headers map[string]string) string {
	t.Helper()
	f := fiber.New(fiberConfig(cfg, 1<<20))
	f.Get("/", func(c fiber.Ctx) error { return c.SendString(c.IP()) })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	go func() {
		_ = f.Listener(&peerListener{Listener: ln, peer: &net.TCPAddr{IP: net.ParseIP(peer), Port: 443}}, _listenConfig)
	}()
	t.Cleanup(func() { _ = f.Shutdown() })

	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/", nil)
	require.NoError(t, err)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return string(b)
}

func TestCloudflareClientIP(t *testing.T) {
	cf := AppConfig{Cloudflare: true}
	visitor := map[string]string{"CF-Connecting-IP": "203.0.113.7"}

	assert.Equal(t, "203.0.113.7", clientIP(t, cf, "173.245.48.1", visitor), "from a Cloudflare IPv4 edge")
	assert.Equal(t, "203.0.113.7", clientIP(t, cf, "2606:4700::1", visitor), "from a Cloudflare IPv6 edge")
	assert.Equal(t, "198.51.100.9", clientIP(t, cf, "198.51.100.9", visitor), "a direct client can't spoof it")
	assert.Equal(t, "173.245.48.1", clientIP(t, cf, "173.245.48.1", map[string]string{"X-Forwarded-For": "203.0.113.7"}), "only CF-Connecting-IP counts")
	assert.Equal(t, "173.245.48.1", clientIP(t, cf, "173.245.48.1", map[string]string{"CF-Connecting-IP": "not-an-ip"}), "an invalid header falls back to the peer")
	assert.Equal(t, "198.51.100.9", clientIP(t, AppConfig{}, "198.51.100.9", visitor), "off by default")

	// A local proxy in front of the app is trusted alongside Cloudflare.
	local := AppConfig{Cloudflare: true, TrustedProxies: "10.0.0.0/8"}
	assert.Equal(t, "203.0.113.7", clientIP(t, local, "10.0.0.2", visitor))
}

func TestCloudflareRanges(t *testing.T) {
	for _, r := range _cloudflareRanges {
		p, err := netip.ParsePrefix(r)
		require.NoError(t, err, r)
		assert.Equal(t, p.Masked().String(), r, "a canonical range")
	}
	assert.Len(t, _cloudflareRanges, 22)
}
