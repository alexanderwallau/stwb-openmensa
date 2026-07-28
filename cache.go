package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const cacheFileName = "menus.json"

type diskCacheEntry struct {
	Data    []byte    `json:"data"`
	Expires time.Time `json:"expires"`
}

// newCache restores unexpired entries from cacheDir. An empty cacheDir keeps
// the cache in memory only, which is useful for ephemeral deployments.
func newCache(cacheDir string) (*cache, error) {
	c := &cache{items: make(map[string]cacheEntry)}
	if cacheDir == "" {
		return c, nil
	}

	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}
	c.cacheFile = filepath.Join(cacheDir, cacheFileName)
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *cache) load() error {
	f, err := os.Open(c.cacheFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open cache file: %w", err)
	}
	defer f.Close()

	var diskItems map[string]diskCacheEntry
	if err := json.NewDecoder(f).Decode(&diskItems); err != nil {
		return fmt.Errorf("decode cache file: %w", err)
	}
	now := time.Now()
	for key, entry := range diskItems {
		if now.Before(entry.Expires) {
			c.items[key] = cacheEntry{data: entry.Data, expires: entry.Expires}
		}
	}
	return nil
}

// saveLocked writes a complete replacement file, then renames it atomically so
// an interruption during a write never leaves a partial cache behind.
func (c *cache) saveLocked() error {
	if c.cacheFile == "" {
		return nil
	}
	diskItems := make(map[string]diskCacheEntry, len(c.items))
	for key, entry := range c.items {
		diskItems[key] = diskCacheEntry{Data: entry.data, Expires: entry.expires}
	}

	f, err := os.CreateTemp(filepath.Dir(c.cacheFile), ".menus-*.tmp")
	if err != nil {
		return err
	}
	tmpName := f.Name()
	defer os.Remove(tmpName)
	if err := f.Chmod(0o640); err != nil {
		f.Close()
		return err
	}
	if err := json.NewEncoder(f).Encode(diskItems); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, c.cacheFile)
}
