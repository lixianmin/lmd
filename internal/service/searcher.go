package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

// ftsSafeRe 移除 FTS5 不认识的字符，只保留字母、数字、空格
var ftsSafeRe = regexp.MustCompile(`[^a-zA-Z0-9\s\p{Han}\p{Katakana}\p{Hiragana}]`) 

type Searcher struct {
	tokenizer tokenizer.Tokenizer
}

func NewSearcher(tok tokenizer.Tokenizer) *Searcher {
	return &Searcher{tokenizer: tok}
}

func (my *Searcher) SearchLex(query, collection string, limit int, minScore float64, strategy string) ([]formatter.SearchHit, error) {
	// 中文分词: gse tokenizer 将 CJK 文本切分为空格分隔的词语
	tokenized := query
	if my.tokenizer != nil {
		t := my.tokenizer.TokenizeToString(query)
		if t != "" {
			tokenized = t
		}
	}

	var ftsQuery string
	switch strategy {
	case "or":
		ftsQuery = buildFTSQuery(tokenized)
	case "df", "":
		ftsQuery = my.buildFTSQueryDF(tokenized)
	case "and":
		ftsQuery = buildFTSQueryAND(tokenized)
	default:
		ftsQuery = my.buildFTSQueryDF(tokenized)
	}
	if ftsQuery == "" {
		return nil, nil
	}

	// "and" 策略使用 QMD 风格的 bm25() 评分
	var ftsResults []dao.FTSSearchResult
	var err error
	if strategy == "and" {
		ftsResults, err = dao.SearchFTSBM25(ftsQuery, collection, limit)
	} else {
		ftsResults, err = dao.SearchFTS(ftsQuery, collection, limit)
	}
	if err != nil {
		return nil, err
	}

	var hits []formatter.SearchHit
	for _, r := range ftsResults {
		if r.Score < minScore {
			continue
		}

		hits = append(hits, formatter.SearchHit{
			ChunkId:    r.ChunkID,
			DocId:      dao.ShortDocId(r.DocId),
			Collection: r.Collection,
			Path:       r.Path,
			Title:      r.Title,
			Score:      r.Score,
			Snippet:    r.Content,
			Line:       r.Line,
		})
	}

	return hits, nil
}

// buildFTSQuery 参考 VBFS agent-memory-store: 去非字母数字、分词、去单字、去停用词、OR 连接
func buildFTSQuery(raw string) string {
	// 1. 保留字母数字和空格（含 CJK）
	s := ftsSafeRe.ReplaceAllString(raw, " ")
	// 2. 按空白分词
	words := strings.Fields(s)
	// 3. 去单字 + 去英文停用词
	var terms []string
	for _, w := range words {
		if len(w) > 1 && !isStopWord(w) {
			terms = append(terms, w)
		}
	}
	if len(terms) == 0 {
		return ""
	}
	// 4. OR 连接（上限 256 个词防止查询过长）
	if len(terms) > 256 {
		terms = terms[:256]
	}
	return strings.Join(terms, " OR ")
}

// buildFTSQueryAND QMD风格: AND + 前缀 * + 不去停用词
// 参考 qmd/src/store.ts buildFTS5Query
func buildFTSQueryAND(raw string) string {
	s := ftsSafeRe.ReplaceAllString(raw, " ")
	words := strings.Fields(s)
	var terms []string
	for _, w := range words {
		if len(w) > 1 {
			terms = append(terms, `"`+w+`"*`)
		}
	}
	if len(terms) == 0 {
		return ""
	}
	return strings.Join(terms, " AND ")
}

// isStopWord 判断是否为英文停用词
func isStopWord(w string) bool {
	_, ok := enStopWords[w]
	return ok
}

// stopWords 中文+英文停用词
var stopWords = map[string]struct{}{
	// 英文停用词
	"a": {}, "an": {}, "the": {}, "is": {}, "are": {}, "was": {}, "were": {},
	"be": {}, "been": {}, "being": {}, "have": {}, "has": {}, "had": {}, "having": {},
	"do": {}, "does": {}, "did": {}, "doing": {}, "will": {}, "would": {}, "shall": {},
	"should": {}, "can": {}, "could": {}, "may": {}, "might": {}, "must": {},
	"i": {}, "me": {}, "my": {}, "we": {}, "our": {}, "you": {}, "your": {},
	"he": {}, "she": {}, "it": {}, "they": {}, "them": {},
	"this": {}, "that": {}, "these": {}, "those": {},
	"what": {}, "which": {}, "who": {}, "whom": {}, "when": {}, "where": {}, "why": {}, "how": {},
	"to": {}, "of": {}, "in": {}, "for": {}, "on": {}, "with": {}, "at": {}, "by": {},
	"from": {}, "about": {}, "as": {}, "into": {}, "like": {}, "through": {},
	"after": {}, "over": {}, "between": {}, "out": {}, "up": {}, "down": {}, "off": {},
	"and": {}, "but": {}, "or": {}, "if": {}, "because": {}, "so": {},
	"than": {}, "too": {}, "very": {}, "just": {}, "now": {}, "then": {}, "also": {},
	"not": {}, "no": {}, "only": {},
	"here": {}, "there": {},
	"all": {}, "each": {}, "every": {}, "both": {}, "few": {}, "more": {}, "most": {},
	"other": {}, "some": {}, "such": {}, "own": {}, "same": {},
	// 中文停用词
	"的": {}, "了": {}, "在": {}, "是": {}, "我": {}, "有": {}, "和": {}, "就": {},
	"不": {}, "人": {}, "都": {}, "一": {}, "一个": {}, "上": {}, "也": {}, "很": {},
	"到": {}, "说": {}, "要": {}, "去": {}, "你": {}, "会": {}, "着": {}, "没有": {},
	"看": {}, "好": {}, "自己": {}, "这": {}, "他": {}, "她": {}, "它": {}, "们": {},
	"那": {}, "什么": {}, "怎么": {}, "哪": {}, "吗": {}, "啊": {}, "呢": {}, "吧": {},
	"能": {}, "可以": {}, "知道": {}, "觉得": {}, "这个": {}, "那个": {}, "想": {},
	"做": {}, "让": {}, "把": {}, "被": {}, "给": {}, "对": {}, "从": {}, "用": {},
	"因为": {}, "所以": {}, "但是": {}, "如果": {}, "虽然": {}, "还是": {}, "已经": {},
	"应该": {}, "可能": {}, "一定": {}, "这样": {}, "那样": {}, "为什么": {},
}

// enStopWords 向后兼容别名
var enStopWords = stopWords

// buildFTSQueryDF 稀有词提取: 优先用 GSE 内置 IDF（快），无 IDF 时 fallback 到 SQL COUNT
func (my *Searcher) buildFTSQueryDF(raw string) string {
	s := ftsSafeRe.ReplaceAllString(raw, " ")
	words := strings.Fields(s)
	var terms []string
	for _, w := range words {
		if len(w) > 1 && !isStopWord(w) {
			terms = append(terms, w)
		}
	}
	if len(terms) == 0 {
		return ""
	}

	type wordDF struct {
		word string
		df   float64 // 越小越稀有（IDF 越大越稀有，COUNT 越小越稀有）
	}
	var candidates []wordDF
	for _, w := range terms {
		idf := my.tokenizer.GetIDF(w)
		if idf > 0 {
			candidates = append(candidates, wordDF{w, idf})
		} else {
			// GSE IDF 不支持英文，fallback 到 SQL COUNT
			cnt := dao.GetTermCount(w)
			if cnt > 0 {
				candidates = append(candidates, wordDF{w, 1.0 / float64(cnt+1)})
			}
		}
	}
	if len(candidates) == 0 {
		return ""
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].df > candidates[j].df // IDF越大越稀有，1/COUNT越大越稀有
	})
	// 取最稀有的 5 个词
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

func (my *Searcher) SearchVector(ctx context.Context, provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	logo.Info("SearchVector: query=%q collection=%s limit=%d", query, collection, limit)
	queryVec, err := provider.EmbedQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	hits, err := my.SearchVectorByEmbedding(queryVec, collection, limit)
	if err != nil {
		return nil, err
	}
	if minScore > 0 {
		var filtered []formatter.SearchHit
		for _, h := range hits {
			if h.Score >= minScore {
				filtered = append(filtered, h)
			}
		}
		return filtered, nil
	}
	return hits, nil
}

const vectorOverfetchFactor = 5 // 向量搜索全局取回后按 collection 过滤，需放大取回量以保证足够结果

func (my *Searcher) SearchVectorByEmbedding(queryVec []float32, collection string, limit int) ([]formatter.SearchHit, error) {
	fetchLimit := limit
	if collection != "" {
		fetchLimit = limit * vectorOverfetchFactor
	}

	vecResults, err := dao.QueryVectors(queryVec, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("vector query failed: %w", err)
	}

	if len(vecResults) == 0 {
		return nil, nil
	}

	chunkIds := make([]int64, len(vecResults))
	for i, r := range vecResults {
		chunkIds[i] = r.ChunkId
	}

	chunks, err := dao.GetChunksByIds(chunkIds)
	if err != nil {
		return nil, fmt.Errorf("fetch chunks failed: %w", err)
	}

	docIds := make(map[int64]struct{})
	for _, c := range chunks {
		docIds[c.DocId] = struct{}{}
	}
	docIdSlice := make([]int64, 0, len(docIds))
	for id := range docIds {
		docIdSlice = append(docIdSlice, id)
	}
	docs, err := dao.GetDocumentsByIds(docIdSlice)
	if err != nil {
		return nil, fmt.Errorf("fetch docs failed: %w", err)
	}

	chunkMap := make(map[int64]*dao.ChunkRecord)
	for i := range chunks {
		chunkMap[chunks[i].Id] = &chunks[i]
	}
	docMap := make(map[int64]*dao.DocumentRecord)
	for i := range docs {
		docMap[docs[i].Id] = &docs[i]
	}

	distanceMap := make(map[int64]float64)
	for _, r := range vecResults {
		distanceMap[r.ChunkId] = r.Distance
	}

	var hits []formatter.SearchHit
	for _, r := range vecResults {
		chunk, ok := chunkMap[r.ChunkId]
		if !ok {
			continue
		}
		doc, ok := docMap[chunk.DocId]
		if !ok {
			continue
		}

		if collection != "" && doc.Collection != collection {
			continue
		}

		hits = append(hits, formatter.SearchHit{
			ChunkId:    r.ChunkId,
			DocId:      dao.ShortDocId(doc.DocId),
			Collection: doc.Collection,
			Path:       doc.Path,
			Title:      doc.Title,
			Score:      dao.SimilarityToScore(distanceMap[r.ChunkId]),
			Snippet:    chunk.Content,
			Line:       chunk.Position,
		})
	}

	return hits, nil
}
