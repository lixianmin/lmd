package daemon

import "net/http"

type route struct {
	method  string
	path    string
	handler func(d *Daemon, w http.ResponseWriter, r *http.Request)
}

func registerRoutes(d *Daemon) http.Handler {
	mux := http.NewServeMux()

	routes := []route{
		{"GET", "/health", (*Daemon).handleHealth},
		{"POST", "/search", (*Daemon).handleSearch},
		{"POST", "/vsearch", (*Daemon).handleVsearch},
		{"POST", "/query", (*Daemon).handleQuery},
		{"POST", "/hyde", (*Daemon).handleHyde},
		{"POST", "/smart-query", (*Daemon).handleSmartQuery},
		{"POST", "/get", (*Daemon).handleGet},
		{"GET", "/status", (*Daemon).handleStatus},
		{"POST", "/collection/add", (*Daemon).handleCollectionAdd},
		{"POST", "/collection/remove", (*Daemon).handleCollectionRemove},
		{"GET", "/collection/list", (*Daemon).handleCollectionList},
		{"POST", "/collection/rename", (*Daemon).handleCollectionRename},
		{"POST", "/rebuild", (*Daemon).handleRebuild},
		{"POST", "/memory/add", (*Daemon).handleMemoryAdd},
		{"POST", "/memory/delete", (*Daemon).handleMemoryDelete},
		{"POST", "/memory/update", (*Daemon).handleMemoryUpdate},
		{"POST", "/mcp", (*Daemon).handleMCP},
	}

	for _, rt := range routes {
		h := rt.handler
		if rt.method == "GET" {
			mux.HandleFunc(rt.path, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
					return
				}
				h(d, w, r)
			})
		} else {
			mux.HandleFunc(rt.path, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
					return
				}
				h(d, w, r)
			})
		}
	}

	return mux
}
