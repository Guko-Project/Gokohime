package randpic

import (
	"sort"
	"testing"

	"github.com/colanns/gokohime/internal/database"
	"github.com/colanns/gokohime/internal/testutil/dbtest"
)

func TestAvailableRandPicCategoriesPrefersDatabase(t *testing.T) {
	tx := dbtest.BeginTx(t)
	dbtest.ResetModels(t, tx, &database.RandPicItem{})
	dbtest.UseAsGlobal(t, tx)

	if err := tx.Create(&database.RandPicItem{
		Category: "fu",
		FileName: "fu.png",
		FilePath: "data/randpic/fu/fu.png",
		FileHash: "hash-fu",
	}).Error; err != nil {
		t.Fatalf("seed fu randpic item failed: %v", err)
	}
	if err := tx.Create(&database.RandPicItem{
		Category: "dly",
		FileName: "dly.png",
		FilePath: "data/randpic/dly/dly.png",
		FileHash: "hash-dly",
	}).Error; err != nil {
		t.Fatalf("seed dly randpic item failed: %v", err)
	}

	got := availableRandPicCategories()
	sort.Strings(got)
	if len(got) != 2 || got[0] != "dly" || got[1] != "fu" {
		t.Fatalf("unexpected categories: %#v", got)
	}

	aliases := directCommandAliases()
	if aliases["fu"] != "fu" || aliases["fufu"] != "fu" || aliases["dly"] != "dly" {
		t.Fatalf("unexpected direct command aliases: %#v", aliases)
	}
}
