package realtime

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// hungRedis accepts connections and never answers, like a Redis that is
// up at the network level but stuck.
func hungRedis(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
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

func TestPublisher_NeverBlocksTheWrite(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: hungRedis(t), ContextTimeoutEnabled: true})
	defer rdb.Close()
	p := NewPublisher(rdb)
	ctx := t.Context()
	go p.Run(ctx)

	start := time.Now()
	for range 50 {
		p.Ring(uuid.New(), 1)
		p.ProjectChanged(uuid.New())
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Errorf("100 rings took %v with Redis hung; a write must never wait on Redis", d)
	}
}

func TestOriginPatterns(t *testing.T) {
	tests := []struct {
		cors string
		want []string
	}{
		{"http://localhost:3000", []string{"localhost:3000"}},
		{"https://tasks.example.com", []string{"tasks.example.com"}},
		{"", nil},
		{"not a url", nil},
	}
	for _, tt := range tests {
		got := OriginPatterns(tt.cors)
		if len(got) != len(tt.want) || (len(got) == 1 && got[0] != tt.want[0]) {
			t.Errorf("OriginPatterns(%q) = %v, want %v", tt.cors, got, tt.want)
		}
	}
}
