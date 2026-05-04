package service

import (
	"strings"
	"testing"

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
	svc := NewMemoryService()

	id, err := svc.Add("user prefers dark mode")
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}
}

func TestMemoryAddEmptyContent(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService()

	_, err := svc.Add("")
	if err == nil {
		t.Fatal("expected error for empty content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMemoryDelete(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService()

	id, _ := svc.Add("to be deleted")

	results, _ := svc.List(10)
	found := false
	for _, r := range results {
		if r.ID == id {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("memory should exist before delete")
	}

	if err := svc.Delete(id); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	results, _ = svc.List(10)
	for _, r := range results {
		if r.ID == id {
			t.Fatal("memory should not appear after delete")
		}
	}
}

func TestMemoryDeleteNotFound(t *testing.T) {
	initMemoryTestDB(t)

	err := dao.DeleteMemory(99999)
	if err == nil {
		t.Fatal("expected error for nonexistent memory")
	}
}

func TestMemoryUpdate(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService()

	id, _ := svc.Add("original content")

	if err := svc.Update(id, "updated content"); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	rec, _ := dao.GetMemoryByID(id)
	if rec == nil {
		t.Fatal("memory should exist after update")
	}
	if rec.Content != "updated content" {
		t.Fatalf("expected 'updated content', got %q", rec.Content)
	}
}

func TestMemoryUpdateEmptyContent(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService()

	err := svc.Update(1, "")
	if err == nil {
		t.Fatal("expected error for empty content")
	}
	if !strings.Contains(err.Error(), "content is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMemoryList(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService()

	svc.Add("first memory")
	svc.Add("second memory")
	svc.Add("third memory")

	results, err := svc.List(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 2 {
		t.Fatalf("expected at most 2 results, got %d", len(results))
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].ID <= 0 {
		t.Fatalf("expected positive id, got %d", results[0].ID)
	}
	if results[0].Content == "" {
		t.Fatal("expected non-empty content")
	}
	if results[0].CreatedAt == "" {
		t.Fatal("expected non-empty created_at")
	}
}
