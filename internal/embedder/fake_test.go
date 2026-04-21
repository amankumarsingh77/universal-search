package embedder

import (
	"context"
	"math"
	"testing"
)

func TestFakeEmbedder_Determinism(t *testing.T) {
	f := NewFake("fake", 8)
	a, err := f.EmbedQuery(context.Background(), "hello")
	if err != nil {
		t.Fatalf("EmbedQuery err: %v", err)
	}
	b, err := f.EmbedQuery(context.Background(), "hello")
	if err != nil {
		t.Fatalf("EmbedQuery err: %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("length mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic at index %d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestFakeEmbedder_DifferentInputs(t *testing.T) {
	f := NewFake("fake", 8)
	a, _ := f.EmbedQuery(context.Background(), "hello")
	b, _ := f.EmbedQuery(context.Background(), "world")
	equal := true
	for i := range a {
		if a[i] != b[i] {
			equal = false
			break
		}
	}
	if equal {
		t.Fatal("expected different vectors for different inputs")
	}
}

func TestFakeEmbedder_Dimensions(t *testing.T) {
	f := NewFake("fake", 16)
	if f.Dimensions() != 16 {
		t.Fatalf("Dimensions()=%d want 16", f.Dimensions())
	}
	if f.ModelID() != "fake" {
		t.Fatalf("ModelID()=%q want fake", f.ModelID())
	}
	v, _ := f.EmbedQuery(context.Background(), "x")
	if len(v) != 16 {
		t.Fatalf("len(v)=%d want 16", len(v))
	}
}

func TestFakeEmbedder_EmbedBatch(t *testing.T) {
	f := NewFake("fake", 8)
	inputs := []ChunkInput{
		{Title: "a", Text: "one"},
		{Title: "b", Text: "two"},
		{Title: "c", Text: "three"},
	}
	got, err := f.EmbedBatch(context.Background(), inputs)
	if err != nil {
		t.Fatalf("EmbedBatch err: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got)=%d want 3", len(got))
	}
	// element 1 equals EmbedQuery of the same effective key
	key := inputs[1].Title + "|" + inputs[1].Text + "|" + string(inputs[1].Data)
	want, _ := f.EmbedQuery(context.Background(), key)
	for i := range want {
		if got[1][i] != want[i] {
			t.Fatalf("batch vs query mismatch at %d: %v vs %v", i, got[1][i], want[i])
		}
	}
}

func TestFakeEmbedder_UnitLength(t *testing.T) {
	f := NewFake("fake", 32)
	v, _ := f.EmbedQuery(context.Background(), "anything")
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(sum)-1.0) > 1e-5 {
		t.Fatalf("vector not unit-length: norm=%v", math.Sqrt(sum))
	}
}
