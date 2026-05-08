package service

import (
	"testing"
)

func TestRouteQueryEmpty(t *testing.T) {
	router := NewTopicRouter()
	collections, docIDs, err := router.Route(nil)
	if err != nil {
		t.Fatalf("Route failed: %v", err)
	}
	if len(collections) != 0 {
		t.Errorf("expected 0 collections, got %d", len(collections))
	}
	if len(docIDs) != 0 {
		t.Errorf("expected 0 docIDs, got %d", len(docIDs))
	}
}
