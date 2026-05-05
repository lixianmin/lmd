package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ─── Dataset Types ─────────────────────────────────────────────────────────

type Turn struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	HasAnswer bool   `json:"has_answer"`
}

type Question struct {
	Question         string   `json:"question"`
	QuestionType     string   `json:"question_type"`
	HaystackSessions [][]Turn `json:"haystack_sessions"`
}

// ─── Metrics ──────────────────────────────────────────────────────────────

func recallAtK(retrieved []int, relevant map[int]bool, k int) float64 {
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

func ndcgAtK(retrieved []int, relevant map[int]bool, k int) float64 {
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

func getRelevantSessionIds(q Question) map[int]bool {
	relevant := make(map[int]bool)
	for i, session := range q.HaystackSessions {
		for _, turn := range session {
			if turn.HasAnswer {
				relevant[i] = true
				break
			}
		}
	}
	return relevant
}

// ─── Dataset Loading ──────────────────────────────────────────────────────

const datasetURL = "https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_s_cleaned.json"

func loadDataset(cacheDir string) ([]Question, error) {
	cacheFile := filepath.Join(cacheDir, "longmemeval_s_cleaned.json")
	if data, err := os.ReadFile(cacheFile); err == nil {
		fmt.Fprintf(os.Stderr, "[bench] Loading cached dataset: %s\n", cacheFile)
		var questions []Question
		return questions, json.Unmarshal(data, &questions)
	}

	fmt.Fprintf(os.Stderr, "[bench] Downloading dataset from HuggingFace...\n")
	resp, err := http.Get(datasetURL)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	os.MkdirAll(cacheDir, 0755)
	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[bench] Failed to cache dataset: %v\n", err)
	}

	var questions []Question
	return questions, json.Unmarshal(data, &questions)
}

// ─── Query Builder ────────────────────────────────────────────────────────

var nonWord = regexp.MustCompile(`[^\w\s]`)

type wordDF struct {
	word string
	df   int
}

// buildQuery extracts the 5 rarest keywords and builds an OR query
func buildQuery(db *sql.DB, question string) string {
	clean := nonWord.ReplaceAllString(question, " ")
	clean = regexp.MustCompile(`\s+`).ReplaceAllString(clean, " ")
	words := strings.Fields(clean)

	// Compute DF for each candidate word
	var candidates []wordDF
	for _, w := range words {
		if len(w) <= 2 {
			continue
		}
		var cnt int
		row := db.QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE content MATCH ?", w)
		if err := row.Scan(&cnt); err != nil {
			continue
		}
		candidates = append(candidates, wordDF{w, cnt})
	}

	if len(candidates) == 0 {
		// Fallback: use first 3 words of length > 2
		for _, w := range words {
			if len(w) > 2 {
				return w
			}
		}
		return "test"
	}

	// Sort by DF ascending (rarest first), take top 5
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].df != candidates[j].df {
			return candidates[i].df < candidates[j].df
		}
		return candidates[i].word < candidates[j].word
	})

	n := 5
	if len(candidates) < n {
		n = len(candidates)
	}
	selected := make([]string, n)
	for i := 0; i < n; i++ {
		selected[i] = candidates[i].word
	}
	return strings.Join(selected, " OR ")
}

// ─── Benchmark ────────────────────────────────────────────────────────────

type BenchmarkResult struct {
	Mode      string
	Recall5   float64
	Recall10  float64
	NDCG10    float64
	Questions int
	Elapsed   time.Duration
}

func runBenchmark(questions []Question, mode string) BenchmarkResult {
	result := BenchmarkResult{Mode: mode}
	startTime := time.Now()
	validCount := 0

	for qi, q := range questions {
		relevantIds := getRelevantSessionIds(q)
		if len(relevantIds) == 0 {
			continue
		}

		db, err := sql.Open("sqlite", ":memory:?_pragma=tokenize=porter")
		if err != nil {
			continue
		}

		_, err = db.Exec("CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(content, tokenize='porter unicode61')")
		if err != nil {
			db.Close()
			continue
		}

		// Index sessions
		idToSession := make(map[int]int)
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
			res, err := db.Exec("INSERT INTO chunks_fts(content) VALUES (?)", content)
			if err != nil {
				continue
			}
			chunkId, _ := res.LastInsertId()
			idToSession[int(chunkId)] = si
		}

		// Build keyword query + search
		query := buildQuery(db, q.Question)
		rows, err := db.Query(
			"SELECT rowid FROM chunks_fts WHERE content MATCH ? ORDER BY rank LIMIT 10",
			query,
		)
		if err != nil {
			db.Close()
			continue
		}

		var retrievedSessionIds []int
		seen := make(map[int]bool)
		for rows.Next() {
			var rowId int
			rows.Scan(&rowId)
			sid, ok := idToSession[rowId]
			if ok && !seen[sid] {
				seen[sid] = true
				retrievedSessionIds = append(retrievedSessionIds, sid)
			}
		}
		rows.Close()
		db.Close()

		r5 := recallAtK(retrievedSessionIds, relevantIds, 5)
		r10 := recallAtK(retrievedSessionIds, relevantIds, 10)
		n10 := ndcgAtK(retrievedSessionIds, relevantIds, 10)

		result.Recall5 += r5
		result.Recall10 += r10
		result.NDCG10 += n10
		validCount++

		if (qi+1)%50 == 0 {
			elapsed := time.Since(startTime).Seconds()
			fmt.Fprintf(os.Stderr, "\r[bench] %d/%d | R@5=%.1f%% (%.1fs)", qi+1, len(questions), result.Recall5/float64(validCount)*100, elapsed)
		}
	}

	if validCount > 0 {
		result.Recall5 /= float64(validCount)
		result.Recall10 /= float64(validCount)
		result.NDCG10 /= float64(validCount)
	}
	result.Questions = validCount
	result.Elapsed = time.Since(startTime)

	return result
}

// ─── Main ─────────────────────────────────────────────────────────────────

func main() {
	cacheDir := os.Getenv("HOME") + "/.cache/lmd/benchmarks"
	limit := 0

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--limit":
			fmt.Sscanf(os.Args[i+1], "%d", &limit)
		}
	}

	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println(" LongMemEval Benchmark — LMD")
	fmt.Println(" Search: BM25 (FTS5 with DF-based keyword extraction)")
	fmt.Printf(" Date: %s\n", time.Now().Format("2006-01-02"))
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()

	questions, err := loadDataset(cacheDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load dataset: %v\n", err)
		os.Exit(1)
	}

	if limit > 0 && limit < len(questions) {
		questions = questions[:limit]
	}
	fmt.Printf(" Questions: %d\n\n", len(questions))

	result := runBenchmark(questions, "bm25")

	fmt.Println()
	fmt.Println(" Mode    | Recall@5 | Recall@10 | NDCG@10 | Questions")
	fmt.Println(" --------|----------|-----------|---------|----------")
	fmt.Printf(" %-7s |   %5.1f%% |    %5.1f%% |  %6.3f | %d\n",
		"bm25", result.Recall5*100, result.Recall10*100, result.NDCG10, result.Questions)

	fmt.Println("\n Comparison with published systems:")
	fmt.Printf("   %-35s | Recall@5 | LLM\n", "System")
	fmt.Println("   -------------------------------------|----------|----------")
	comparisons := []struct{ name, recall5, llm string }{
		{"MemPalace hybrid+LLM", "100.0%", "Haiku"},
		{"MemPalace raw", "96.6%", "None"},
		{"Mastra (GPT-4o-mini)", "94.87%", "Yes"},
		{"agent-memory-store (hybrid)", "92.1%", "None"},
		{"agent-memory-store (bm25)", "92.0%", "None"},
		{"Hindsight (Gemini)", "91.4%", "Yes"},
		{"Stella (dense)", "~85%", "None"},
		{"Contriever", "~78%", "None"},
		{"BM25 (sparse)", "~70%", "None"},
	}
	for _, c := range comparisons {
		fmt.Printf("   %-35s | %8s | %-8s\n", c.name, c.recall5, c.llm)
	}
	fmt.Println("   -------------------------------------|----------|----------")
	fmt.Printf("   %-35s |   %5.1f%% | None     ◀\n", "lmd (bm25 — this run)", result.Recall5*100)

	fmt.Printf("\n Completed in %.1fs\n", result.Elapsed.Seconds())
}
