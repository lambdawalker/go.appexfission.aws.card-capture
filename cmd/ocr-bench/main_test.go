package main

import "testing"

func TestPercentiles(t *testing.T) {
	v := []int64{100, 1, 20, 3}
	if percentile(v, .5) != 3 || percentile(v, .95) != 100 || v[0] != 100 || percentile(nil, .5) != 0 {
		t.Fatal("percentiles")
	}
}
