package service

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
)

func initMemoryTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := dir + "/test.sqlite"
	if err := dao.Init(dbPath); err != nil {
		t.Fatalf("dao.Init: %v", err)
	}
	t.Cleanup(func() {
		dao.DB.Close()
		dao.DB = nil
	})
}

func TestMemoryAdd(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil)

	id, err := svc.Add("user prefers dark mode", "fact")
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
}

func TestMemorySearchNoDecay(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil)

	svc.Add("user prefers dark mode", "fact")
	svc.Add("light theme is default", "fact")

	results, err := svc.Search("dark", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].Score <= 0 {
		t.Fatalf("expected positive score, got %f", results[0].Score)
	}
	if results[0].Type != "fact" {
		t.Fatalf("expected type fact, got %s", results[0].Type)
	}
}

func TestMemorySearchFactNoDecay(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil)

	id, _ := svc.Add("important fact here", "fact")
	oldTime := time.Now().Add(-365 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	dao.WithExec("UPDATE memories SET created_at=? WHERE id=?", oldTime, id)

	results, err := svc.Search("important fact", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].Score <= 0 {
		t.Fatalf("fact should have no decay, got score %f", results[0].Score)
	}
}

func TestMemorySearchEpisodeDecay(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil)

	id, _ := svc.Add("episode event happened", "episode")
	oldTime := time.Now().Add(-15 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	dao.WithExec("UPDATE memories SET created_at=? WHERE id=?", oldTime, id)

	results, err := svc.Search("episode event", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	rec, _ := dao.GetMemoryByID(id)
	ageDays := time.Since(rec.CreatedAt).Hours() / 24
	expectedDecay := 0.5
	approx := results[0].Score / results[0].RawScore
	if approx < expectedDecay-0.1 || approx > expectedDecay+0.1 {
		t.Fatalf("expected decay factor ~0.5 for 15-day episode, got %.3f (age=%.1f days)", approx, ageDays)
	}
}

func TestMemorySearchRelationDecay(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil)

	id, _ := svc.Add("user likes coffee", "relation")
	oldTime := time.Now().Add(-180 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	dao.WithExec("UPDATE memories SET created_at=? WHERE id=?", oldTime, id)

	results, err := svc.Search("coffee", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	approx := results[0].Score / results[0].RawScore
	if approx < 0.4 || approx > 0.6 {
		t.Fatalf("expected decay factor ~0.5 for 180-day relation, got %.3f", approx)
	}
}

func TestMemorySearchFilterByType(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil)

	svc.Add("dark mode preference", "fact")
	svc.Add("dark sky tonight", "episode")

	results, err := svc.Search("dark", 10, "fact")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Type != "fact" {
			t.Fatalf("expected only fact type, got %s", r.Type)
		}
	}
}

func TestMemoryAddInvalidType(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil)

	_, err := svc.Add("test", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	if !strings.Contains(err.Error(), "invalid memory type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecayFactor(t *testing.T) {
	tests := []struct {
		memType string
		ageDays float64
		want    float64
		delta   float64
	}{
		{"fact", 365, 1.0, 0},
		{"fact", 0, 1.0, 0},
		{"episode", 15, 0.5, 0.01},
		{"episode", 30, 0.25, 0.01},
		{"episode", 0, 1.0, 0.01},
		{"relation", 180, 0.5, 0.01},
		{"relation", 360, 0.25, 0.01},
		{"relation", 0, 1.0, 0.01},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%.0fd", tt.memType, tt.ageDays), func(t *testing.T) {
			got := decayFactor(tt.memType, tt.ageDays)
			if got < tt.want-tt.delta || got > tt.want+tt.delta {
				t.Fatalf("decayFactor(%s, %.0f) = %.4f, want %.4f±%.4f", tt.memType, tt.ageDays, got, tt.want, tt.delta)
			}
		})
	}
}
