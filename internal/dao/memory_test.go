package dao

import (
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

func makeSerializedVec(t *testing.T) []byte {
	t.Helper()
	vec := make([]float32, EmbeddingDim)
	for i := range vec {
		vec[i] = 0.01
	}
	serialized, err := sqlite_vec.SerializeFloat32(vec)
	if err != nil {
		t.Fatal(err)
	}
	return serialized
}

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

	vec := make([]float32, EmbeddingDim)
	vec[0] = 1.0
	serialized, err := sqlite_vec.SerializeFloat32(vec)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateMemoryEmbedding(id, serialized); err != nil {
		t.Fatal(err)
	}

	rec, err := GetMemoryByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Embedding) == 0 {
		t.Fatal("expected embedding to be set")
	}

	count := 0
	DB.db.QueryRow("SELECT COUNT(*) FROM memories_vec WHERE memory_id=?", id).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 row in memories_vec, got %d", count)
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

	UpdateMemoryEmbedding(results[0].ID, makeSerializedVec(t))

	if count := GetUnembeddedMemoryCount(); count != 1 {
		t.Fatalf("expected 1 unembedded after update, got %d", count)
	}
}

func TestSearchMemoryVector(t *testing.T) {
	initTestDB(t)

	InsertMemory("user likes coffee", "relation")
	InsertMemory("python is great", "fact")

	results, err := GetUnembeddedMemories(10)
	if err != nil {
		t.Fatal(err)
	}

	UpdateMemoryEmbedding(results[0].ID, makeSerializedVec(t))

	queryVec := make([]float32, EmbeddingDim)
	for i := range queryVec {
		queryVec[i] = 0.01
	}

	vecResults, err := SearchMemoryVector(queryVec, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(vecResults) == 0 {
		t.Fatal("expected at least 1 vector result")
	}
	if vecResults[0].ID != results[0].ID {
		t.Fatalf("expected memory %d, got %d", results[0].ID, vecResults[0].ID)
	}
}
