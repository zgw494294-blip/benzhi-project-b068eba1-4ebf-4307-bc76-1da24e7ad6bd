package seedpool

import (
	"context"
	"testing"
	"time"
)

func TestRunSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := RunSmoke(ctx); err != nil {
		t.Fatal(err)
	}
}
