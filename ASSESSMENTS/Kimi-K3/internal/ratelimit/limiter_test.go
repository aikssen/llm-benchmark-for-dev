package ratelimit

import (
	"testing"
	"time"
)

func TestLimiterAllowsUpToLimit(t *testing.T) {
	l := New(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("call %d should be allowed", i+1)
		}
	}
	if l.Allow("k") {
		t.Fatal("fourth call should be denied")
	}
}

func TestLimiterRefillsOverTime(t *testing.T) {
	l := New(2, time.Second)
	now := time.Now()
	l.now = func() time.Time { return now }

	if !l.Allow("k") || !l.Allow("k") {
		t.Fatal("first two calls should pass")
	}
	if l.Allow("k") {
		t.Fatal("third call should be denied")
	}

	now = now.Add(time.Second) // one full window elapsed
	if !l.Allow("k") {
		t.Fatal("tokens should have refilled after one window")
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l := New(1, time.Minute)
	if !l.Allow("a") {
		t.Fatal("a should pass")
	}
	if l.Allow("a") {
		t.Fatal("a should be limited")
	}
	if !l.Allow("b") {
		t.Fatal("b has its own bucket and should pass")
	}
}
