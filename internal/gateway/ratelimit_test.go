package gateway

import "testing"

func TestRateLimiterBurstThenDeny(t *testing.T) {
	// rate 0/s so the bucket never refills during the test; burst of 3.
	rl := NewRateLimiter(0, 3)

	for i := 0; i < 3; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("request %d within burst should be allowed", i+1)
		}
	}
	if rl.Allow("1.2.3.4") {
		t.Fatal("4th request beyond burst should be denied")
	}
}

func TestRateLimiterIsolatesKeys(t *testing.T) {
	rl := NewRateLimiter(0, 1)
	if !rl.Allow("a") {
		t.Fatal("first key should be allowed")
	}
	if !rl.Allow("b") {
		t.Fatal("a different key must have its own bucket")
	}
}
