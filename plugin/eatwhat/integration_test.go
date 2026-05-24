package eatwhat

import (
	"testing"

	"github.com/colanns/gokohime/internal/database"
	"github.com/colanns/gokohime/internal/testutil/dbtest"
)

func TestMealStoreIntegration(t *testing.T) {
	tx := dbtest.BeginTx(t)
	dbtest.ResetModels(t, tx, &database.MealEntry{})
	dbtest.UseAsGlobal(t, tx)

	if err := addMeal("eatwhat", "炸鸡"); err != nil {
		t.Fatalf("addMeal failed: %v", err)
	}

	exists, err := checkMealInList("eatwhat", "炸鸡")
	if err != nil {
		t.Fatalf("checkMealInList failed: %v", err)
	}
	if !exists {
		t.Fatalf("expected meal to exist after add")
	}

	meal, err := getRandomMeal("eatwhat")
	if err != nil {
		t.Fatalf("getRandomMeal failed: %v", err)
	}
	if meal != "炸鸡" {
		t.Fatalf("unexpected meal: %q", meal)
	}

	if err := delMeal("eatwhat", "炸鸡"); err != nil {
		t.Fatalf("delMeal failed: %v", err)
	}
	meal, err = getRandomMeal("eatwhat")
	if err != nil {
		t.Fatalf("getRandomMeal after delete failed: %v", err)
	}
	if meal != "西北风" {
		t.Fatalf("expected fallback meal after delete, got %q", meal)
	}
}
