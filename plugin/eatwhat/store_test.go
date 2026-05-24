package eatwhat

import (
	"errors"
	"testing"

	"github.com/colanns/gokohime/internal/database"
	"gorm.io/gorm"
)

func TestResolveRandomMeal(t *testing.T) {
	meal, err := resolveRandomMeal(nil, gorm.ErrRecordNotFound)
	if err != nil {
		t.Fatalf("resolveRandomMeal returned error: %v", err)
	}
	if meal != "西北风" {
		t.Fatalf("expected fallback meal 西北风, got %q", meal)
	}

	meal, err = resolveRandomMeal(&database.MealEntry{Name: "烤肉"}, nil)
	if err != nil {
		t.Fatalf("resolveRandomMeal returned error: %v", err)
	}
	if meal != "烤肉" {
		t.Fatalf("unexpected meal: %q", meal)
	}

	wantErr := errors.New("boom")
	if _, err := resolveRandomMeal(nil, wantErr); !errors.Is(err, wantErr) {
		t.Fatalf("expected original error to be returned")
	}
}
