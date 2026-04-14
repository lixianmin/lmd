package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/spf13/cobra"
)

var (
	getFull  bool
	getFrom  int
	getLines int
)

var getCmd = &cobra.Command{
	Use:   "get <path-or-docid>",
	Short: "Get a document by path or docid",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		input := args[0]
		var doc *store.DocumentRecord

		if strings.HasPrefix(input, "#") {
			doc, err = store.GetDocumentByDocID(db, input[1:])
		} else {
			parts := strings.SplitN(input, "/", 2)
			if len(parts) == 2 {
				doc, err = store.GetDocumentByPath(db, parts[0], parts[1])
			} else {
				matches, searchErr := store.SearchDocumentsByPath(db, input, 10)
				if searchErr != nil {
					return fmt.Errorf("invalid path format, use collection/path or #docid: %s", input)
				}
				if len(matches) > 0 {
					fmt.Fprintf(os.Stderr, "No exact match. Similar files:\n")
					for _, m := range matches {
						fmt.Fprintf(os.Stderr, "  %s/%s\n", m.Collection, m.Path)
					}
					return nil
				}
				return fmt.Errorf("invalid path format, use collection/path or #docid: %s", input)
			}

			if err != nil {
				matches, searchErr := store.SearchDocumentsByPath(db, parts[1], 10)
				if searchErr == nil && len(matches) > 0 {
					fmt.Fprintf(os.Stderr, "Document not found: %s\n\nSimilar files:\n", input)
					for _, m := range matches {
						fmt.Fprintf(os.Stderr, "  %s/%s\n", m.Collection, m.Path)
					}
					return nil
				}
			}
		}

		if err != nil {
			return fmt.Errorf("document not found: %s", input)
		}

		fmt.Printf("#%s %s\n", store.ShortDocID(doc.DocID), doc.Title)
		fmt.Printf("Collection: %s\n", doc.Collection)
		fmt.Printf("Path: %s\n", doc.Path)
		fmt.Printf("Size: %d bytes\n", doc.FileSize)
		fmt.Println()

		body := doc.Body
		if !getFull {
			if len(body) > 500 {
				body = body[:500] + "..."
			}
		}
		if getFrom > 0 {
			lines := strings.Split(body, "\n")
			if getFrom <= len(lines) {
				body = strings.Join(lines[getFrom-1:], "\n")
			}
		}
		if getLines > 0 {
			lines := strings.Split(body, "\n")
			if getLines < len(lines) {
				body = strings.Join(lines[:getLines], "\n")
			}
		}

		fmt.Println(body)
		return nil
	},
}

func init() {
	getCmd.Flags().BoolVar(&getFull, "full", false, "show full document")
	getCmd.Flags().IntVar(&getFrom, "from", 0, "start from line number")
	getCmd.Flags().IntVarP(&getLines, "lines", "l", 0, "max lines to show")
	rootCmd.AddCommand(getCmd)
}
