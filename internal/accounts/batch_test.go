package accounts

import (
	"slices"
	"testing"
)

func TestBatchNormalizesSelectionAndRejectsUnsafeInputs(t *testing.T) {
	input := BatchInput{Key: "batch", Action: "ban", IDs: []int64{3, 2, 3}}
	got, err := normalizedBatch(input)
	if err != nil || !slices.Equal(got.IDs, []int64{2, 3}) {
		t.Fatalf("normalization %+v %v", got, err)
	}
	if !slices.Equal(input.IDs, []int64{3, 2, 3}) {
		t.Fatal("input selection mutated")
	}
	for _, invalid := range []BatchInput{
		{Key: "", Action: "kick", IDs: []int64{1}},
		{Key: " key ", Action: "kick", IDs: []int64{1}},
		{Key: "batch", Action: "kick", IDs: []int64{0}},
		{Key: "batch", Action: "kick", IDs: []int64{}},
		{Key: "batch", Action: "kick", IDs: make([]int64, 101)},
		{Key: "batch", Action: "unknown", IDs: []int64{1}},
	} {
		if _, err := normalizedBatch(invalid); err == nil {
			t.Fatalf("invalid batch accepted %+v", invalid)
		}
	}
}
