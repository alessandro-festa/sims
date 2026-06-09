package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildCards_SPXDefault(t *testing.T) {
	cards := buildCards(3, "MI300X", 1<<37, "spx", 1)
	if len(cards) != 3 {
		t.Fatalf("len = %d, want 3", len(cards))
	}
	for i, c := range cards {
		if c.Index != i {
			t.Errorf("cards[%d].Index = %d, want %d", i, c.Index, i)
		}
		if c.PartitionMode != "spx" || c.PartitionID != "0" {
			t.Errorf("cards[%d] partition = %s/%s, want spx/0", i, c.PartitionMode, c.PartitionID)
		}
	}
}

func TestBuildCards_CPXMultiplies(t *testing.T) {
	cards := buildCards(2, "MI300X", 1000, "cpx", 4)
	if len(cards) != 8 {
		t.Fatalf("len = %d, want 8 (2 physical × 4 partitions)", len(cards))
	}
	// VRAM is split evenly across partitions: 1000/4 = 250.
	if cards[0].VRAMTotalB != 250 {
		t.Errorf("VRAM split: got %d, want 250", cards[0].VRAMTotalB)
	}
	// Partition IDs cycle 0..3 twice.
	wantIDs := []string{"0", "1", "2", "3", "0", "1", "2", "3"}
	for i, c := range cards {
		if c.PartitionID != wantIDs[i] {
			t.Errorf("cards[%d].PartitionID = %q, want %q", i, c.PartitionID, wantIDs[i])
		}
	}
}

func TestBuildCards_UnknownModeFallsBackToSPX(t *testing.T) {
	cards := buildCards(2, "MI300X", 1<<37, "weird", 5)
	if len(cards) != 2 {
		t.Errorf("unknown mode should reduce to spx (1 per physical); got %d", len(cards))
	}
	for _, c := range cards {
		if c.PartitionMode != "spx" {
			t.Errorf("expected spx fallback, got %s", c.PartitionMode)
		}
	}
}

func TestPrintJSON_ParsesAndHasExpectedKeys(t *testing.T) {
	cards := buildCards(2, "MI300X", 1<<37, "spx", 1)
	var buf bytes.Buffer
	printJSON(&buf, cards)

	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("output not valid JSON: %v\nbody:\n%s", err, buf.String())
	}
	if _, ok := doc["system"]; !ok {
		t.Error("missing top-level system key")
	}
	for i := range cards {
		key := "card" + string(rune('0'+i))
		raw, ok := doc[key]
		if !ok {
			t.Errorf("missing %s key", key)
			continue
		}
		card, _ := raw.(map[string]any)
		for _, required := range []string{
			"Card Series", "Card Vendor", "Serial Number",
			"Temperature (Sensor edge) (C)",
			"Average Graphics Package Power (W)",
			"Performance Level",
		} {
			if _, ok := card[required]; !ok {
				t.Errorf("%s: missing key %q", key, required)
			}
		}
	}
}

func TestPrintTable_ContainsExpectedRows(t *testing.T) {
	cards := buildCards(2, "MI300X", 1<<37, "spx", 1)
	var buf bytes.Buffer
	printTable(&buf, cards)
	out := buf.String()

	// Real rocm-smi's table includes these markers; consumers grep for them.
	for _, must := range []string{
		"ROCm System Management Interface",
		"Concise Info",
		"End of ROCm SMI Log",
		"GPU",
		"Temp",
		"AvgPwr",
	} {
		if !strings.Contains(out, must) {
			t.Errorf("table output missing %q. full:\n%s", must, out)
		}
	}
	// One data row per card (above the closing markers).
	lines := strings.Split(out, "\n")
	dataRows := 0
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "0 ") || strings.HasPrefix(strings.TrimSpace(l), "1 ") {
			dataRows++
		}
	}
	if dataRows < len(cards) {
		t.Errorf("table output had %d data rows, want at least %d", dataRows, len(cards))
	}
}
