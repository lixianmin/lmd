package service

import (
	"strings"
	"testing"
)

const sampleTopicMD = `# 数据库优化

> 本目录包含数据库性能优化相关资料，涵盖索引策略、查询优化、分库分表。

## 关键主题
- MySQL 索引优化
- 查询计划分析

## 文档
- mysql-index.md — MySQL索引类型与使用场景
- query-plan.md — EXPLAIN 输出解读

## 语义分组
- **索引策略** (2篇): mysql-index.md, btree-hash.md
- **查询优化** (1篇): query-plan.md
`

func TestParseTopicMD(t *testing.T) {
	topic, err := parseTopicMD(sampleTopicMD)
	if err != nil {
		t.Fatalf("parseTopicMD failed: %v", err)
	}
	if topic.Title != "数据库优化" {
		t.Errorf("title: got %q", topic.Title)
	}
	if !strings.Contains(topic.Overview, "数据库性能优化") {
		t.Errorf("overview mismatch: %s", topic.Overview)
	}
	if len(topic.Documents) != 2 {
		t.Errorf("documents: want 2, got %d", len(topic.Documents))
	}
	if topic.Documents[0].Path != "mysql-index.md" {
		t.Errorf("doc[0].path: got %q", topic.Documents[0].Path)
	}
	if topic.Documents[0].Desc != "MySQL索引类型与使用场景" {
		t.Errorf("doc[0].desc: got %q", topic.Documents[0].Desc)
	}
	if len(topic.SemanticGroups) != 2 {
		t.Errorf("semantic groups: want 2, got %d", len(topic.SemanticGroups))
	}
	if topic.SemanticGroups[0].Name != "索引策略" {
		t.Errorf("group[0].name: got %q", topic.SemanticGroups[0].Name)
	}
}

func TestBuildSummarizePrompt(t *testing.T) {
	docs := []docPreview{
		{Path: "a.md", Title: "Title A", Preview: "Content of document A"},
		{Path: "b.md", Title: "Title B", Preview: "Content of document B"},
	}
	prompt := buildSummarizePrompt("/notes/db", docs)
	if !strings.Contains(prompt, "/notes/db") {
		t.Error("prompt missing dir path")
	}
	if !strings.Contains(prompt, "Title A") {
		t.Error("prompt missing doc title")
	}
}

func TestShouldSummarize(t *testing.T) {
	if !ShouldSummarize("abc123", "abc123") {
		t.Error("should re-summarize when hash matches")
	}
	if ShouldSummarize("abc123", "def456") {
		t.Error("should skip when hash changed (human edited)")
	}
	if !ShouldSummarize("", "new123") {
		t.Error("should summarize on first run")
	}
}
