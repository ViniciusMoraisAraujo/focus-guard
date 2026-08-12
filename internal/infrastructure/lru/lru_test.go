package lru

import "testing"

func TestCache_GetMiss(t *testing.T) {
	c := New[string](4)
	if _, ok := c.Get("nope"); ok {
		t.Fatal("Get de chave ausente deveria devolver ok=false")
	}
	if c.Len() != 0 {
		t.Fatalf("Len = %d, want 0", c.Len())
	}
}

func TestCache_SetGet(t *testing.T) {
	c := New[int](4)
	c.Set("a", 1)
	c.Set("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %d/%v, want 1/true", v, ok)
	}
	if v, ok := c.Get("b"); !ok || v != 2 {
		t.Fatalf("Get(b) = %d/%v, want 2/true", v, ok)
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
}

func TestCache_UpdateMovesToFront(t *testing.T) {
	c := New[int](3)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)
	// Toca em "a" (vira a mais recente); inserir "d" evicta "b" (a menos
	// recente AGORA), não "a".
	c.Get("a")
	c.Set("d", 4)
	if _, ok := c.Get("b"); ok {
		t.Error("b deveria ter sido evictada (LRU) após o teto")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := c.Get(k); !ok {
			t.Errorf("%s deveria continuar no cache", k)
		}
	}
}

func TestCache_EvictsOldestWhenFull(t *testing.T) {
	c := New[int](2)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3) // evicta "a"
	if _, ok := c.Get("a"); ok {
		t.Error("a deveria ter sido evictada com o teto 2")
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
	// Re-inserir uma chave evictada volta a ocupar espaço e evicta a menos
	// recente (b).
	c.Set("a", 1)
	if _, ok := c.Get("b"); ok {
		t.Error("b deveria ter sido evictada ao re-inserir a")
	}
}

func TestCache_NewClampsToAtLeastOne(t *testing.T) {
	c := New[int](0)
	c.Set("a", 1)
	if c.Len() != 1 {
		t.Fatalf("Len = %d, want 1 (teto mínimo)", c.Len())
	}
}
