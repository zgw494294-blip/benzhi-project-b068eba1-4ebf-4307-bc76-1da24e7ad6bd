package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRunDefaultsToBoundedStartupCheck(t *testing.T) {
	done := make(chan error, 1)
	var stdout, stderr bytes.Buffer
	go func() {
		done <- run(nil, &stdout, &stderr)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned an error: %v\nstderr: %s", err, stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("default startup check did not terminate")
	}
	if !strings.Contains(stdout.String(), "seedpool startup: ok") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
