package persistence

import (
	"fmt"
	"strings"
	"testing"

	"retrom/internal/model"
)

func TestContentCacheSnapshotsAndEntryBound(t *testing.T) {
	cache := newContentCache()
	files := []model.ContentFile{{LogicalKey: "original"}}
	cache.put("first", files)
	files[0].LogicalKey = "caller edit"
	read := cache.get("first")
	if read[0].LogicalKey != "original" {
		t.Fatal("cache retained caller's mutable slice")
	}
	read[0].LogicalKey = "reader edit"
	if cache.get("first")[0].LogicalKey != "original" {
		t.Fatal("cache exposed its own slice")
	}
	for i := range 16 {
		cache.put(fmt.Sprint(i), files)
	}
	if cache.get("first") != nil || len(cache.entries) != 16 {
		t.Fatal("directory entry limit did not evict oldest package")
	}
}

func TestContentCacheByteBound(t *testing.T) {
	cache := newContentCache()
	files := make([]model.ContentFile, 20_000, 40_000)
	for i := range files {
		files[i].LogicalKey = strings.Repeat("x", 1024)
	}
	cache.put("one", files)
	cache.put("two", files)
	if cache.bytes > 32*1024*1024 || cache.get("one") != nil || cache.get("two") == nil {
		t.Fatal("cache byte limit did not evict oldest directory")
	}
	files = append(files, files...)
	cache.put("oversized", files)
	if cache.get("oversized") != nil || cache.get("two") == nil {
		t.Fatal("oversized directory entered cache or evicted a usable entry")
	}
}
