package main

import "testing"

func TestBootstrapCanary(t *testing.T) {
	t.Fatal("intentional bootstrap verification failure")
}
