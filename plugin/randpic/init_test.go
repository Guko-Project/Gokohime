package randpic

import "testing"

func TestIsSupportedRandPicFilename(t *testing.T) {
	cases := map[string]bool{
		"ok.png":              true,
		"ok.JPG":              true,
		"nested.webp":         true,
		"skip.txt":            false,
		"foo.Zone.Identifier": false,
		"archive.zip":         false,
		"":                    false,
	}

	for name, want := range cases {
		if got := isSupportedRandPicFilename(name); got != want {
			t.Fatalf("isSupportedRandPicFilename(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLookupDirectCategoryCommand(t *testing.T) {
	if category, ok := lookupDirectCategoryCommand("fufu"); !ok || category != "fu" {
		t.Fatalf("lookupDirectCategoryCommand(fufu) = (%q, %v), want (fu, true)", category, ok)
	}
	if category, ok := lookupDirectCategoryCommand("save"); ok || category != "" {
		t.Fatalf("lookupDirectCategoryCommand(save) = (%q, %v), want (\"\", false)", category, ok)
	}
}
