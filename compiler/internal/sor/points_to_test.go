package sor

import (
	"testing"
)

func TestPointsToSet_AddRelation(t *testing.T) {
	pts := NewPointsToSet()

	// Simulate: release src -> [holder_a, holder_b]
	pts.AddRelation("holder_a", "src")
	pts.AddRelation("holder_b", "src")

	// Check GetSharedSource
	if source := pts.GetSharedSource("holder_a"); source != "src" {
		t.Errorf("GetSharedSource(holder_a) = %q, want %q", source, "src")
	}
	if source := pts.GetSharedSource("holder_b"); source != "src" {
		t.Errorf("GetSharedSource(holder_b) = %q, want %q", source, "src")
	}

	// Check GetSharedHolders
	holders := pts.GetSharedHolders("src")
	if len(holders) != 2 {
		t.Fatalf("Expected 2 holders, got %d", len(holders))
	}
}

func TestPointsToSet_IsHolder(t *testing.T) {
	pts := NewPointsToSet()
	pts.AddRelation("holder_a", "src")

	if !pts.IsHolder("holder_a") {
		t.Error("IsHolder(holder_a) should be true")
	}
	if pts.IsHolder("src") {
		t.Error("IsHolder(src) should be false")
	}
}

func TestPointsToSet_EstimatePoolAdjustment(t *testing.T) {
	pts := NewPointsToSet()
	pts.AddRelation("holder_a", "src")
	pts.AddRelation("holder_b", "src")

	sizes := map[string]int{
		"holder_a": 100,
		"holder_b": 100,
		"src":      200,
	}

	// holder_b is a duplicate (same source), should be subtracted
	adjustment := pts.EstimatePoolAdjustment(sizes)
	if adjustment >= 0 {
		t.Errorf("Expected negative adjustment, got %d", adjustment)
	}
}

func TestBuildPointsToSet(t *testing.T) {
	tracker := NewOwnershipTracker()

	// Create a source object
	srcID := tracker.NewObject("src", "int64", false, 1)

	// Create holder objects
	holderAID := tracker.NewObject("holder_a", "int64", false, 2)
	holderBID := tracker.NewObject("holder_b", "int64", false, 3)

	// Release src to holders
	tracker.Release(srcID, []string{"holder_a", "holder_b"}, 4)

	pts := BuildPointsToSet(tracker)

	// Check that holders point to src
	if source := pts.GetSharedSource(holderAID); source != srcID {
		t.Errorf("GetSharedSource(holder_a) = %q, want %q", source, srcID)
	}
	if source := pts.GetSharedSource(holderBID); source != srcID {
		t.Errorf("GetSharedSource(holder_b) = %q, want %q", source, srcID)
	}
}

func TestFormatPointsToSummary(t *testing.T) {
	pts := NewPointsToSet()
	pts.AddRelation("holder_a", "src")

	summary := FormatPointsToSummary(pts)
	if summary == "" {
		t.Error("FormatPointsToSummary returned empty string")
	}
}
