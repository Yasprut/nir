package cache

import (
	"fmt"
	"testing"
)

func TestLFU_BasicGetPut(t *testing.T) {
	c := NewLFU[string, int](3)

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("expected a=1, got %d ok=%v", v, ok)
	}
	if _, ok := c.Get("z"); ok {
		t.Fatal("expected miss for z")
	}
}

func TestLFU_EvictsLeastFrequent(t *testing.T) {
	c := NewLFU[string, int](3)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)

	// Access "a" and "b" twice so they have higher freq than "c"
	c.Get("a")
	c.Get("a")
	c.Get("b")
	c.Get("b")

	// Adding "d" should evict "c" (freq=1, oldest)
	c.Put("d", 4)

	if _, ok := c.Get("c"); ok {
		t.Fatal("c should have been evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a should still be present")
	}
	if _, ok := c.Get("d"); !ok {
		t.Fatal("d should be present")
	}
}

func TestLFU_Update(t *testing.T) {
	c := NewLFU[string, int](2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("a", 99) // update existing key

	if v, ok := c.Get("a"); !ok || v != 99 {
		t.Fatalf("expected a=99 after update, got %d ok=%v", v, ok)
	}
}

func TestLFU_Flush(t *testing.T) {
	c := NewLFU[string, int](5)
	for i := 0; i < 5; i++ {
		c.Put(fmt.Sprintf("k%d", i), i)
	}
	c.Flush()
	if c.Len() != 0 {
		t.Fatalf("expected empty cache after Flush, got Len=%d", c.Len())
	}
	if _, ok := c.Get("k0"); ok {
		t.Fatal("expected miss after Flush")
	}
}

func TestLFU_Capacity(t *testing.T) {
	c := NewLFU[int, int](3)
	for i := 0; i < 10; i++ {
		c.Put(i, i)
	}
	if c.Len() > 3 {
		t.Fatalf("cache exceeded capacity: Len=%d", c.Len())
	}
}
