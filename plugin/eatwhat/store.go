package eatwhat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/colanns/gokohime/internal/database"
	"gorm.io/gorm"
)

func ensureMealDataFile() error {
	return nil
}

func addMeal(kind, meal string) error {
	meal = normalizeMeal(meal)
	if meal == "" {
		return errors.New("meal is empty")
	}
	return database.AddMealEntry(context.Background(), nil, kind, meal, hashMeal(kind, meal))
}

func checkMealInList(kind, meal string) (bool, error) {
	return database.MealExists(context.Background(), nil, kind, normalizeMeal(meal))
}

func delMeal(kind, meal string) error {
	return database.DeleteMealEntry(context.Background(), nil, kind, normalizeMeal(meal))
}

func getRandomMeal(kind string) (string, error) {
	entry, err := database.RandomMealEntry(context.Background(), nil, kind)
	return resolveRandomMeal(entry, err)
}

func resolveRandomMeal(entry *database.MealEntry, err error) (string, error) {
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "西北风", nil
		}
		return "", err
	}
	if entry == nil {
		return "西北风", nil
	}
	return entry.Name, nil
}

func normalizeMeal(meal string) string {
	return strings.TrimSpace(meal)
}

func hashMeal(kind, meal string) string {
	sum := sha256.Sum256([]byte(kind + "\n" + meal))
	return hex.EncodeToString(sum[:])
}
