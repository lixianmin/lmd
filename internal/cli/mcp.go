package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/mcp"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server (stdio)",
	RunE: func(cmd *cobra.Command, args []string) error {
		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return err
		}

		searcher := service.NewSearcher(tok)
		provider := newProvider()
		defer provider.Close()

		mcp.RegisterHandler(func(name string, params json.RawMessage) (interface{}, error) {
			return handleToolCall(searcher, provider, name, params)
		})

		mcp.Serve(os.Stdin, os.Stdout)
		return nil
	},
}

func handleToolCall(searcher *service.Searcher, provider embedding.EmbeddingProvider, name string, params json.RawMessage) (interface{}, error) {
	switch name {
	case "search":
		var p struct {
			Query      string  `json:"query"`
			Collection string  `json:"collection,omitempty"`
			Limit      int     `json:"limit,omitempty"`
			MinScore   float64 `json:"min_score,omitempty"`
		}
		json.Unmarshal(params, &p)
		if p.Limit == 0 {
			p.Limit = 5
		}
		return searcher.SearchHybrid(provider, p.Query, p.Collection, p.Limit, p.MinScore)

	case "search_lex":
		var p struct {
			Query      string `json:"query"`
			Collection string `json:"collection,omitempty"`
			Limit      int    `json:"limit,omitempty"`
		}
		json.Unmarshal(params, &p)
		if p.Limit == 0 {
			p.Limit = 5
		}
		return searcher.SearchLex(p.Query, p.Collection, p.Limit, 0)

	case "search_vector":
		var p struct {
			Query      string `json:"query"`
			Collection string `json:"collection,omitempty"`
			Limit      int    `json:"limit,omitempty"`
		}
		json.Unmarshal(params, &p)
		if p.Limit == 0 {
			p.Limit = 5
		}
		return searcher.SearchVector(provider, p.Query, p.Collection, p.Limit, 0)

	case "get":
		var p struct {
			PathOrDocId string `json:"path_or_docid"`
		}
		json.Unmarshal(params, &p)
		return getDocumentResult(p.PathOrDocId)

	case "status":
		return dao.ListCollections()

	case "list_collections":
		return dao.ListCollections()

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func getDocumentResult(pathOrDocId string) (interface{}, error) {
	if len(pathOrDocId) > 0 && pathOrDocId[0] == '#' {
		return dao.GetDocumentByDocId(pathOrDocId[1:])
	}
	parts := strings.SplitN(pathOrDocId, "/", 2)
	if len(parts) == 2 {
		return dao.GetDocumentByPath(parts[0], parts[1])
	}
	return nil, fmt.Errorf("invalid path format, use collection/path or #docid")
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
