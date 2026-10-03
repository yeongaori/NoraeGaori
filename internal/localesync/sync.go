package localesync

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"noraegaori/internal/logger"
)

type BaselineStore interface {
	Load(lang string) ([]byte, bool, error)
	Save(lang string, content []byte) error
}

func Sync(dir string, shipped fs.FS, store BaselineStore) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	names, _ := fs.Glob(shipped, "*.json")

	var failures []error
	for _, name := range names {
		if err := syncLocale(dir, shipped, name, store); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
		}
	}
	return errors.Join(failures...)
}

func syncLocale(dir string, shipped fs.FS, name string, store BaselineStore) error {
	lang := strings.TrimSuffix(name, ".json")
	shippedData, err := fs.ReadFile(shipped, name)
	if err != nil {
		return fmt.Errorf("failed to read the shipped locale: %w", err)
	}
	shippedTree, err := parseTree(shippedData)
	if err != nil {
		return fmt.Errorf("the shipped locale is not valid JSON: %w", err)
	}

	path := filepath.Join(dir, name)
	userData, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := writeFile(path, shippedData); err != nil {
			return err
		}
		logger.Infof("Created %s", path)
		return store.Save(lang, shippedData)
	}
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	userTree, err := parseTree(userData)
	if err != nil {
		return fmt.Errorf("%s is not valid JSON, so it was left unchanged: %w", path, err)
	}

	baselineTree, err := loadBaseline(store, lang)
	if err != nil {
		return err
	}
	merged := mergeLocale(shippedTree, baselineTree, userTree)
	if !equalNodes(merged, userTree) {
		if err := writeFile(path, encodeTree(merged)); err != nil {
			return err
		}
		logger.Infof("Updated %s with this version's strings, keeping your edits", path)
	}
	return store.Save(lang, shippedData)
}

func loadBaseline(store BaselineStore, lang string) (*node, error) {
	data, found, err := store.Load(lang)
	if err != nil {
		return nil, fmt.Errorf("failed to load the previous %s locale: %w", lang, err)
	}
	if !found {
		return nil, nil
	}
	baseline, err := parseTree(data)
	if err != nil {
		logger.Warnf("The stored previous %s locale is unreadable, so every changed string counts as your edit: %v", lang, err)
		return nil, nil
	}
	return baseline, nil
}

func writeFile(path string, data []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", temporary, err)
	}
	return os.Rename(temporary, path)
}
