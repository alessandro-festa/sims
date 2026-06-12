package annotations

import (
	"math/rand"
	"testing"
)

func TestParseUtilization_Valid(t *testing.T) {
	cases := []struct {
		in   string
		want Range
	}{
		{"60-80", Range{60, 80}},
		{"0-100", Range{0, 100}},
		{"50-50", Range{50, 50}},
		{" 60 - 80 ", Range{60, 80}}, // whitespace tolerant
	}
	for _, c := range cases {
		got, err := ParseUtilization(c.in)
		if err != nil {
			t.Errorf("ParseUtilization(%q) returned err: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseUtilization(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseUtilization_EmptyDefaultsTo5_15(t *testing.T) {
	got, err := ParseUtilization("")
	if err != nil {
		t.Fatalf("ParseUtilization(empty): %v", err)
	}
	if got.Low != 5 || got.High != 15 {
		t.Errorf("empty defaulted to %v, want {5, 15}", got)
	}
}

func TestParseUtilizationWithDefault_CustomFallback(t *testing.T) {
	got, err := ParseUtilizationWithDefault("", "40-60")
	if err != nil {
		t.Fatalf("ParseUtilizationWithDefault(empty, 40-60): %v", err)
	}
	if got.Low != 40 || got.High != 60 {
		t.Errorf("custom default = %v, want {40, 60}", got)
	}
}

func TestParseUtilization_Invalid(t *testing.T) {
	cases := []string{"100", "abc-def", "10-5", "-5-10", "0-150"}
	for _, c := range cases {
		if _, err := ParseUtilization(c); err == nil {
			t.Errorf("ParseUtilization(%q) returned nil err, expected error", c)
		}
	}
}

func TestSample_InRange(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	r := Range{60, 80}
	for range 100 {
		v := r.Sample(rng)
		if v < 60 || v > 80 {
			t.Errorf("Sample = %v, out of [60, 80]", v)
		}
	}
}

func TestSample_DegenerateRange(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	r := Range{42, 42}
	if got := r.Sample(rng); got != 42 {
		t.Errorf("Sample on degenerate range = %v, want 42", got)
	}
}
