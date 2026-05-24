package cp

import "testing"

func TestParseCPArgs(t *testing.T) {
	names, ok := parseCPArgs("Alice   Bob")
	if !ok {
		t.Fatalf("expected args to parse")
	}
	if names[0] != "Alice" || names[1] != "Bob" {
		t.Fatalf("unexpected parsed names: %#v", names)
	}

	if _, ok := parseCPArgs("Alice"); ok {
		t.Fatalf("expected single name to fail")
	}
}

func TestRenderCPStory(t *testing.T) {
	got := renderCPStory("<攻> 和 <受> 一起散步", "A", "B")
	if got != "A 和 B 一起散步" {
		t.Fatalf("unexpected rendered story: %q", got)
	}
}
