package cli

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/spf13/cobra"
)

type lmeTurn struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	HasAnswer bool   `json:"has_answer"`
}

type lmeQuestion struct {
	Question         string     `json:"question"`
	QuestionType     string     `json:"question_type"`
	HaystackSessions [][]lmeTurn `json:"haystack_sessions"`
}

const lmeDatasetURL = "https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_s_cleaned.json"

var nonWordRx = regexp.MustCompile(`[^\w\s]`)
var multiSpaceRx = regexp.MustCompile(`\s+`)

func benchLongMemEval(cmd *cobra.Command, args []string) error {
	limit := benchLimitInt
	mode := benchModeStr

	// ── Load dataset ──
	cacheDir := filepath.Join(os.Getenv("HOME"), ".cache", "lmd", "benchmarks")
	cacheFile := filepath.Join(cacheDir, "longmemeval_s_cleaned.json")

	var questions []lmeQuestion
	if data, err := os.ReadFile(cacheFile); err == nil {
		fmt.Fprintf(os.Stderr, "[bench] Loading cached dataset\n")
		if err := convert.FromJsonE(data, &questions); err != nil {
			return fmt.Errorf("parse: %w", err)
		}
	} else {
		return fmt.Errorf("dataset not found: %s\n  curl -L -o %s %s", cacheFile, cacheFile, lmeDatasetURL)
	}
	if limit > 0 && limit < len(questions) {
		questions = questions[:limit]
	}

	// ── Init ──
	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		return fmt.Errorf("tokenizer: %w", err)
	}
	embedModel := filepath.Join(os.Getenv("HOME"), ".cache", "lmd", "models", "Qwen3-Embedding-0.6B-Q8_0.gguf")
	provider := embedding.NewLlamaProvider(embedModel, 99, 4, 8)

	modes := []string{mode}
	if mode == "all" {
		modes = []string{"bm25", "semantic", "hybrid"}
	}

	type modeStat struct{ r5, r10, ndcg float64 }
	results := make(map[string]*modeStat)
	for _, m := range modes {
		results[m] = &modeStat{}
	}

	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println(" LongMemEval Benchmark — LMD (Qwen3-Embedding + FTS5)")
	fmt.Printf(" Questions: %d\n", len(questions))
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()

	start := time.Now()
	valid := 0

	for qi, q := range questions {
		relevant := make(map[int]bool)
		for i, s := range q.HaystackSessions {
			for _, t := range s {
				if t.HasAnswer {
					relevant[i] = true
					break
				}
			}
		}
		if len(relevant) == 0 {
			continue
		}

		tempDir, _ := os.MkdirTemp("", "lmd-bench")
		dbPath := filepath.Join(tempDir, "bench.sqlite")
		cleanupDB()
		if err := dao.Init(dbPath); err != nil {
			os.RemoveAll(tempDir)
			continue
		}

		// Index sessions
		chunkIds := make([]int64, 0, len(q.HaystackSessions))
		idToSession := make(map[int64]int) // chunkId → session index
		contents := make([]string, 0, len(q.HaystackSessions))

		for si, session := range q.HaystackSessions {
			var lines []string
			for _, turn := range session {
				role := "User"
				if turn.Role == "assistant" {
					role = "Assistant"
				}
				lines = append(lines, role+": "+turn.Content)
			}
			content := strings.Join(lines, "\n")
			if strings.TrimSpace(content) == "" {
				continue
			}

			docid := fmt.Sprintf("%08d", si)
			hash := benchHash(content)
			title := content
			if len([]rune(title)) > 80 {
				title = string([]rune(title)[:80])
			}

			doc := &dao.DocumentRecord{
				Collection: "bench",
				Path:       "/bench/" + docid,
				DocId:      docid,
				Title:      title,
				Body:       content,
				Hash:       hash,
			}
			if err := dao.UpsertDocument(doc); err != nil {
				continue
			}
			text := tok.TokenizeToString(content)
			if text == "" {
				text = content
			}
			chunks, err := dao.InsertChunks(doc.Id, []dao.ChunkData{{
				Content: content, Position: 1, TokenCount: 0, Hash: hash,
			}}, []string{text})
			if err != nil || len(chunks) == 0 {
				continue
			}
			chunkIds = append(chunkIds, chunks[0].Id)
			idToSession[chunks[0].Id] = si
			contents = append(contents, content)
		}

		// Query preprocessing: extract rare keywords via DF
		clean := multiSpaceRx.ReplaceAllString(nonWordRx.ReplaceAllString(q.Question, " "), " ")
		words := strings.Fields(clean)

		type wordCnt struct{ w string; c int }
		var candidates []wordCnt
		for _, w := range words {
			if len(w) <= 2 {
				continue
			}
			c, err := benchFtsCount("benchcontent", w)
			if err != nil || c == 0 {
				continue
			}
			candidates = append(candidates, wordCnt{w, c})
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].c != candidates[j].c {
				return candidates[i].c < candidates[j].c
			}
			return candidates[i].w < candidates[j].w
		})
		n := 5
		if len(candidates) < n {
			n = len(candidates)
		}
		if n == 0 {
			for _, w := range words {
				if len(w) > 2 {
					candidates = append(candidates, wordCnt{w, 1})
					if len(candidates) >= 3 {
						break
					}
				}
			}
			n = len(candidates)
		}
		sel := make([]string, n)
		for i := 0; i < n; i++ {
			sel[i] = candidates[i].w
		}
		bm25Query := strings.Join(sel, " OR ")

		// ── BM25 search ──
		bm25Hits := benchFtsSearch(bm25Query, 30)
		bm25Sessions := dedupSessions(bm25Hits, idToSession)

		// ── Semantic search ──
		var vecSessions []int
		if containsMode(modes, "semantic") || containsMode(modes, "hybrid") {
			vecSessions = benchVectorSearch(provider, q.Question, contents, chunkIds, len(q.HaystackSessions), 30)
		}

		// ── Hybrid (RRF) ──
		var hybridSessions []int
		if containsMode(modes, "hybrid") {
			hybridSessions = benchRRF(bm25Hits, vecSessions, idToSession)
		}

		for _, m := range modes {
			var ret []int
			switch m {
			case "bm25":
				ret = bm25Sessions
			case "semantic":
				ret = vecSessions
			case "hybrid":
				ret = hybridSessions
			}
			results[m].r5 += recallAtKBench(ret, relevant, 5)
			results[m].r10 += recallAtKBench(ret, relevant, 10)
			results[m].ndcg += ndcgAtKBench(ret, relevant, 10)
		}

		cleanupDB()
		os.RemoveAll(tempDir)
		valid++

		if (qi+1)%25 == 0 {
			elapsed := time.Since(start).Seconds()
			r := results["bm25"]
			fmt.Fprintf(os.Stderr, "\r[bench] %d/%d R@5=%.1f%% (%.0fs)", qi+1, len(questions), r.r5/float64(valid)*100, elapsed)
		}
	}

	// ── Output ──
	fmt.Fprintf(os.Stderr, "\n\n")
	fmt.Println(" Mode       | Recall@5 | Recall@10 | NDCG@10 | Q")
	fmt.Println(" -----------|----------|-----------|---------|---")
	for _, m := range modes {
		r := results[m]
		fmt.Printf(" %-10s |   %5.1f%% |    %5.1f%% |  %6.3f | %d\n",
			m, r.r5/float64(valid)*100, r.r10/float64(valid)*100, r.ndcg/float64(valid), valid)
	}
	fmt.Println("\n Comparison:")
	fmt.Printf("   %-35s | Recall@5 | LLM\n", "System")
	fmt.Println("   -------------------------------------|----------|----------")
	for _, c := range []struct{ n, r, l string }{
		{"MemPalace hybrid+LLM", "100.0%", "Haiku"},
		{"MemPalace raw", "96.6%", "None"},
		{"Mastra (GPT-4o-mini)", "94.87%", "Yes"},
		{"agent-memory-store (hybrid)", "92.1%", "None"},
		{"agent-memory-store (bm25)", "92.0%", "None"},
		{"Hindsight (Gemini)", "91.4%", "Yes"},
		{"Stella (dense)", "~85%", "None"},
		{"Contriever", "~78%", "None"},
		{"BM25 (sparse)", "~70%", "None"},
	} {
		fmt.Printf("   %-35s | %8s | %-8s\n", c.n, c.r, c.l)
	}
	fmt.Println("   -------------------------------------|----------|----------")
	for _, m := range modes {
		r := results[m]
		fmt.Printf("   %-35s |   %5.1f%% | None     ◀\n",
			"lmd ("+m+") — Qwen3-Embedding", r.r5/float64(valid)*100)
	}
	fmt.Printf("\n Completed in %.0fs\n", time.Since(start).Seconds())
	return nil
}

// ── Bench-specific FTS helpers (bypass tokenizer) ──

func benchFtsCount(table, word string) (int, error) {
	rows, err := dao.WithQuery("SELECT COUNT(*) FROM chunks_fts WHERE content MATCH ?", word)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var c int
	if rows.Next() {
		rows.Scan(&c)
	}
	return c, rows.Err()
}

func benchFtsSearch(query string, limit int) []struct{ id int64 } {
	rows, err := dao.WithQuery("SELECT rowid FROM chunks_fts WHERE content MATCH ? ORDER BY rank LIMIT ?", query, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var res []struct{ id int64 }
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		res = append(res, struct{ id int64 }{id})
	}
	return res
}

func benchVectorSearch(provider *embedding.LlamaProvider, question string, contents []string, chunkIds []int64, sessionCount, limit int) []int {
	if len(contents) == 0 || provider == nil {
		return nil
	}
	// Truncate each session text to avoid batch overflow (llama batch=4096)
	truncated := make([]string, 0, len(contents))
	for _, c := range contents {
		runes := []rune(c)
		if len(runes) > 500 {
			c = string(runes[:500])
		}
		truncated = append(truncated, c)
	}
	allTexts := append([]string{question}, truncated...)
	fmt.Fprintf(os.Stderr, "\r[bench] embedding %d texts...", len(allTexts))
	vecs, err := provider.EmbedBatch(context.Background(), allTexts)
	fmt.Fprintf(os.Stderr, " done (err=%v, len=%d)\n", err, len(vecs))
	if err != nil || len(vecs) < 2 {
		return nil
	}
	queryVec := vecs[0]
	chunkVecs := vecs[1:]

	type scored struct{ id int; score float64 }
	var items []scored
	for i, cv := range chunkVecs {
		s := cosineSim(queryVec, cv)
		items = append(items, scored{i, s})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].score > items[j].score })

	seen := make(map[int]bool)
	var result []int
	for _, it := range items {
		if len(result) >= limit {
			break
		}
		sid := it.id
		if sid >= sessionCount {
			continue
		}
		// chunkIds[it.id] → session index via idToSession (we use slice index as session)
		// Each session has exactly 1 chunk, so chunk index = session index
		if !seen[sid] {
			seen[sid] = true
			result = append(result, sid)
		}
	}
	return result
}

func benchRRF(bm25Hits []struct{ id int64 }, vecSessions []int, idToSession map[int64]int) []int {
	K := 60.0
	scores := make(map[int]float64)
	for r, h := range bm25Hits {
		sid, ok := idToSession[h.id]
		if !ok {
			continue
		}
		scores[sid] += 0.4 / (K + float64(r) + 1)
	}
	for r, sid := range vecSessions {
		scores[sid] += 0.6 / (K + float64(r) + 1)
	}
	type se struct{ sid int; s float64 }
	var list []se
	for sid, s := range scores {
		list = append(list, se{sid, s})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].s > list[j].s })
	result := make([]int, len(list))
	for i, e := range list {
		result[i] = e.sid
	}
	return result
}

func dedupSessions(hits []struct{ id int64 }, idToSession map[int64]int) []int {
	seen := make(map[int]bool)
	var result []int
	for _, h := range hits {
		sid, ok := idToSession[h.id]
		if !ok {
			continue
		}
		if !seen[sid] {
			seen[sid] = true
			result = append(result, sid)
		}
	}
	return result
}

func cosineSim(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func containsMode(modes []string, m string) bool {
	for _, x := range modes {
		if x == m {
			return true
		}
	}
	return false
}

func cleanupDB() {
	if dao.DB != nil {
		s := dao.DB
		dao.DB = nil
		s.Close()
	}
}

// ── Metrics ──

func recallAtKBench(retrieved []int, relevant map[int]bool, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}
	found := 0
	for i, id := range retrieved {
		if i >= k {
			break
		}
		if relevant[id] {
			found++
		}
	}
	return float64(found) / float64(len(relevant))
}

func ndcgAtKBench(retrieved []int, relevant map[int]bool, k int) float64 {
	if len(relevant) == 0 {
		return 0
	}
	var dcg float64
	for i, id := range retrieved {
		if i >= k {
			break
		}
		if relevant[id] {
			dcg += 1.0 / math.Log2(float64(i)+2)
		}
	}
	idealCount := len(relevant)
	if idealCount > k {
		idealCount = k
	}
	var idcg float64
	for i := 0; i < idealCount; i++ {
		idcg += 1.0 / math.Log2(float64(i)+2)
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

func benchHash(s string) string {
	var h uint32
	for _, c := range s {
		h = h*31 + uint32(c)
	}
	return fmt.Sprintf("%08x", h)
}

var (
	benchLimitInt int
	benchModeStr  string
)

var benchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Run benchmarks",
	RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
}

var benchLongMemEvalCmd = &cobra.Command{
	Use:   "longmemeval",
	Short: "Run LongMemEval benchmark",
	RunE:  benchLongMemEval,
}

func init() {
	benchLongMemEvalCmd.Flags().IntVar(&benchLimitInt, "limit", 0, "Max questions (0=all)")
	benchLongMemEvalCmd.Flags().StringVar(&benchModeStr, "mode", "all", "Mode: bm25|semantic|hybrid|all")
	benchCmd.AddCommand(benchLongMemEvalCmd)
	rootCmd.AddCommand(benchCmd)
}
