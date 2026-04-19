package dao

import (
	"testing"
	"time"
)

func TestInsertMemory(t *testing.T) {
	initTestDB(t)

	id, err := InsertMemory("user prefers dark mode", "fact")
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
}

func TestInsertMemoryDefaultType(t *testing.T) {
	initTestDB(t)

	id, err := InsertMemory("something happened", "episode")
	if err != nil {
		t.Fatal(err)
	}

	rec, err := GetMemoryByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Type != "episode" {
		t.Fatalf("expected type episode, got %s", rec.Type)
	}
}

func TestGetMemoryByID(t *testing.T) {
	initTestDB(t)

	id, err := InsertMemory("test content", "fact")
	if err != nil {
		t.Fatal(err)
	}

	rec, err := GetMemoryByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Content != "test content" {
		t.Fatalf("expected 'test content', got %q", rec.Content)
	}
	if rec.Type != "fact" {
		t.Fatalf("expected fact, got %s", rec.Type)
	}
	if rec.ID != id {
		t.Fatalf("expected id %d, got %d", id, rec.ID)
	}
}

func TestSearchMemoryFTS(t *testing.T) {
	initTestDB(t)

	InsertMemory("user prefers dark mode in editor", "fact")
	InsertMemory("user had lunch at noon", "episode")
	InsertMemory("dark roast coffee is favorite", "relation")

	results, err := SearchMemoryFTS("dark", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results for 'dark', got %d", len(results))
	}

	for _, r := range results {
		if r.Score <= 0 {
			t.Fatalf("expected positive score, got %f", r.Score)
		}
	}
}

func TestSearchMemoryFTSByType(t *testing.T) {
	initTestDB(t)

	InsertMemory("dark mode preference", "fact")
	InsertMemory("dark sky tonight", "episode")

	results, err := SearchMemoryFTSByType("dark", "fact", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Type != "fact" {
		t.Fatalf("expected type fact, got %s", results[0].Type)
	}
}

func TestMemoryFTSScoreDecay(t *testing.T) {
	initTestDB(t)

	id1, _ := InsertMemory("recent event", "episode")

	oldTime := time.Now().Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	_, _ = WithExec(
		"UPDATE memories SET created_at=? WHERE id=?",
		oldTime, id1,
	)

	results, err := SearchMemoryFTS("recent event", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	rec, _ := GetMemoryByID(id1)
	ageDays := time.Since(rec.CreatedAt).Hours() / 24
	if ageDays < 29 {
		t.Fatalf("expected ~30 days old, got %.1f", ageDays)
	}
}

func TestUpdateMemoryEmbedding(t *testing.T) {
	initTestDB(t)

	id, err := InsertMemory("test embedding", "fact")
	if err != nil {
		t.Fatal(err)
	}

	vec := []byte{0, 0, 128, 63, 0, 0, 0, 64}
	if err := UpdateMemoryEmbedding(id, vec); err != nil {
		t.Fatal(err)
	}

	rec, err := GetMemoryByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Embedding) == 0 {
		t.Fatal("expected embedding to be set")
	}
}

func TestGetUnembeddedMemories(t *testing.T) {
	initTestDB(t)

	if count := GetUnembeddedMemoryCount(); count != 0 {
		t.Fatalf("expected 0 unembedded, got %d", count)
	}

	InsertMemory("unembedded memory 1", "fact")
	InsertMemory("unembedded memory 2", "episode")

	if count := GetUnembeddedMemoryCount(); count != 2 {
		t.Fatalf("expected 2 unembedded, got %d", count)
	}

	results, err := GetUnembeddedMemories(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	UpdateMemoryEmbedding(results[0].ID, []byte{1, 2, 3})

	if count := GetUnembeddedMemoryCount(); count != 1 {
		t.Fatalf("expected 1 unembedded after update, got %d", count)
	}
}
