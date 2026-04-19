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
		{"POST", "/get", (*Daemon).handleGet},
		{"GET", "/status", (*Daemon).handleStatus},
		{"POST", "/collection/add", (*Daemon).handleCollectionAdd},
		{"POST", "/collection/remove", (*Daemon).handleCollectionRemove},
		{"GET", "/collection/list", (*Daemon).handleCollectionList},
		{"POST", "/collection/rename", (*Daemon).handleCollectionRename},
		{"POST", "/update", (*Daemon).handleUpdate},
		{"POST", "/embed", (*Daemon).handleEmbed},
		{"POST", "/rebuild", (*Daemon).handleRebuild},
		{"POST", "/memory/add", (*Daemon).handleMemoryAdd},
		{"POST", "/memory/search", (*Daemon).handleMemorySearch},
		{"POST", "/mcp", (*Daemon).handleMCP},
	}

	for _, rt := range routes {
		h := rt.handler
		route := rt
		if route.method == "GET" {
			mux.HandleFunc(route.path, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
					return
				}
				d.touchActivity()
				h(d, w, r)
			})
		} else {
			mux.HandleFunc(route.path, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
					return
				}
				d.touchActivity()
				h(d, w, r)
			})
		}
	}

	return mux
}
