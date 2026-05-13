package cli

import (
	"encoding/json"
	"fmt"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/daemon"
	"github.com/spf13/cobra"
)

var (
	getFull  bool
	getFrom  int
	getLines int
)

var getCmd = &cobra.Command{
Use:   "get <path-or-doc_id>",
Short: "Get a document by path or doc_id",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewClient(config.Cfg.Daemon.Port)
		body, err := client.GetDoc(args[0], getFull, getFrom, getLines)
		if err != nil {
			return err
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Print(string(body))
			return nil
		}

		if errMsg, ok := resp["error"]; ok {
			return fmt.Errorf("%v", errMsg)
		}

		fmt.Printf("#%s %s\n", resp["doc_id"], resp["title"])
		fmt.Printf("Collection: %s\n", resp["collection"])
		fmt.Printf("Path: %s\n", resp["path"])
		fmt.Printf("Size: %.0f bytes\n", resp["file_size"])
		fmt.Println()
		fmt.Println(resp["body"])
		return nil
	},
}

func init() {
	getCmd.Flags().BoolVar(&getFull, "full", false, "show full document")
	getCmd.Flags().IntVar(&getFrom, "from", 0, "start from line number")
	getCmd.Flags().IntVarP(&getLines, "lines", "l", 0, "max lines to show")
	rootCmd.AddCommand(getCmd)
}
