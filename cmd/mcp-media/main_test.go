package main

import (
	"context"
	"testing"
	"time"
)

func TestBoundedContextAddsDeadlineWhenParentHasNone(t *testing.T) {
	ctx, cancel := boundedContext(context.Background(), time.Minute)
	defer cancel()

	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("bounded context did not add a deadline")
	}
}

func TestBoundedContextKeepsEarlierParentDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), time.Minute)
	defer parentCancel()
	parentDeadline, _ := parent.Deadline()

	ctx, cancel := boundedContext(parent, 10*time.Minute)
	defer cancel()
	gotDeadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("bounded context lost parent deadline")
	}
	if !gotDeadline.Equal(parentDeadline) {
		t.Fatalf("deadline = %v, want parent deadline %v", gotDeadline, parentDeadline)
	}
}
