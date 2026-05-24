package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var legacyCategories = map[string]struct{}{
	"fu":     {},
	"gst":    {},
	"gu":     {},
	"kira":   {},
	"motohg": {},
	"pjsk":   {},
	"rui":    {},
	"wt":     {},
	"tls":    {},
}

type summary struct {
	Copied    int               `json:"copied"`
	Skipped   int               `json:"skipped"`
	Errors    []string          `json:"errors,omitempty"`
	Manifest  map[string]string `json:"manifest"`
	Categories map[string]int   `json:"categories"`
}

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		exitf("resolve root: %v", err)
	}

	result, err := mergeSayings(absRoot)
	if err != nil {
		exitf("merge sayings: %v", err)
	}

	output, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		exitf("marshal summary: %v", err)
	}
	fmt.Println(string(output))
}

func mergeSayings(repoRoot string) (*summary, error) {
	srcRoot := filepath.Join(repoRoot, "data", "sayings")
	dstRoot := filepath.Join(repoRoot, "data", "randpic")
	manifestPath := filepath.Join(dstRoot, "_legacy_sayings_manifest.json")

	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		return nil, err
	}

	manifest := map[string]string{}
	if raw, err := os.ReadFile(manifestPath); err == nil {
		_ = json.Unmarshal(raw, &manifest)
	}

	result := &summary{
		Manifest:  manifest,
		Categories: map[string]int{},
	}

	hashIndex := make(map[string]map[string]string)
	for category := range legacyCategories {
		categoryDir := filepath.Join(dstRoot, category)
		if err := os.MkdirAll(categoryDir, 0o755); err != nil {
			return nil, err
		}
		index, err := buildHashIndex(categoryDir, repoRoot)
		if err != nil {
			return nil, err
		}
		hashIndex[category] = index
	}

	err := filepath.WalkDir(srcRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !isSupportedImageFile(d.Name()) {
			return nil
		}

		relFromSrc, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(relFromSrc), "/")
		if len(parts) < 2 {
			return nil
		}
		category := strings.ToLower(strings.TrimSpace(parts[0]))
		if _, ok := legacyCategories[category]; !ok {
			return nil
		}

		sourceRel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		sourceRel = filepath.ToSlash(sourceRel)

		hash, err := fileHash(path)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", sourceRel, err))
			return nil
		}

		if existing, ok := hashIndex[category][hash]; ok {
			result.Manifest[sourceRel] = existing
			result.Skipped++
			return nil
		}

		destDir := filepath.Join(dstRoot, category)
		destName := uniqueDestName(destDir, d.Name(), hash)
		destPath := filepath.Join(destDir, destName)
		if err := copyFile(path, destPath); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", sourceRel, err))
			return nil
		}

		destRel, err := filepath.Rel(repoRoot, destPath)
		if err != nil {
			return err
		}
		destRel = filepath.ToSlash(destRel)
		hashIndex[category][hash] = destRel
		result.Manifest[sourceRel] = destRel
		result.Copied++
		result.Categories[category]++
		return nil
	})
	if err != nil {
		return nil, err
	}

	raw, err := json.MarshalIndent(result.Manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(manifestPath, raw, 0o644); err != nil {
		return nil, err
	}

	return result, nil
}

func buildHashIndex(dir, repoRoot string) (map[string]string, error) {
	index := make(map[string]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return index, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !isSupportedImageFile(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		hash, err := fileHash(path)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return nil, err
		}
		index[hash] = filepath.ToSlash(rel)
	}
	return index, nil
}

func uniqueDestName(dir, originalName, hash string) string {
	name := filepath.Base(originalName)
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return name
	}

	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s_%s%s", base, hash[:8], ext)
}

func isSupportedImageFile(name string) bool {
	name = strings.ToLower(name)
	return strings.HasSuffix(name, ".jpg") ||
		strings.HasSuffix(name, ".jpeg") ||
		strings.HasSuffix(name, ".png") ||
		strings.HasSuffix(name, ".gif") ||
		strings.HasSuffix(name, ".webp")
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func fileHash(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
