package dao

import (
	"encoding/json"
	"testing"
)

func TestUpsertTopic(t *testing.T) {
	initTestDB(t)

	docPaths := []string{"mysql-index.md", "query-plan.md", "btree-hash.md"}
	docPathsJSON, _ := json.Marshal(docPaths)

	err := UpsertTopic("test-coll", "db/", "数据库优化相关资料综述", string(docPathsJSON), "abc123")
	if err != nil {
		t.Fatalf("UpsertTopic failed: %v", err)
	}

	topic, err := GetTopic("test-coll", "db/")
	if err != nil {
		t.Fatalf("GetTopic failed: %v", err)
	}
	if topic.Overview != "数据库优化相关资料综述" {
		t.Errorf("overview mismatch: got %q", topic.Overview)
	}

	var paths []string
	json.Unmarshal([]byte(topic.DocPaths), &paths)
	if len(paths) != 3 {
		t.Errorf("doc_paths len: want 3, got %d", len(paths))
	}
	if topic.Hash != "abc123" {
		t.Errorf("hash mismatch: got %q", topic.Hash)
	}
}

func TestListTopicsByCollection(t *testing.T) {
	initTestDB(t)

	_ = UpsertTopic("test-coll", "", "root overview", `["a.md"]`, "h1")
	_ = UpsertTopic("test-coll", "sub/", "sub overview", `["b.md"]`, "h2")

	topics, err := ListTopicsByCollection("test-coll")
	if err != nil {
		t.Fatalf("ListTopicsByCollection failed: %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("want 2 topics, got %d", len(topics))
	}
}

func TestUpsertTopicOverwrite(t *testing.T) {
	initTestDB(t)

	_ = UpsertTopic("test-coll", "db/", "old overview", `["a.md"]`, "h1")
	_ = UpsertTopic("test-coll", "db/", "new overview", `["a.md","b.md"]`, "h2")

	topic, _ := GetTopic("test-coll", "db/")
	if topic.Overview != "new overview" {
		t.Errorf("overview not updated: got %q", topic.Overview)
	}
	if topic.Hash != "h2" {
		t.Errorf("hash not updated: got %q", topic.Hash)
	}
}
