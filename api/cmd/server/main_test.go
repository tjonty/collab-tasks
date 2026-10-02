package main

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// silentServer accepts TCP connections and never answers, like a dependency
// that is up at the network level but hung.
func silentServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
	)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			c.Close()
		}
	})
	return ln.Addr().String()
}

func TestRedisPing_RespectsContextDeadline(t *testing.T) {
	rdb, err := newRedisClient("redis://" + silentServer(t))
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err = rdb.Ping(ctx).Err()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Ping succeeded against a silent server, want error")
	}
	if elapsed >= time.Second {
		t.Errorf("Ping took %v, want < 1s (the context deadline was ignored)", elapsed)
	}
}
