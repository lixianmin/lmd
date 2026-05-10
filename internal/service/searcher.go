package service

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

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
	var needsPosWeight bool
	switch strategy {
	case "pos-must":
		ftsQuery = my.buildFTSQueryPosMust(query)
	case "pos-weight":
		ftsQuery = buildFTSQuery(tokenized)
		needsPosWeight = true
	case "or":
		ftsQuery = buildFTSQuery(tokenized)
	case "", "pos-or":
		ftsQuery = my.buildFTSQueryPosOr(query)
	case "and":
		ftsQuery = buildFTSQueryAND(tokenized)
	default:
		ftsQuery = buildFTSQuery(tokenized)
	}
	if ftsQuery == "" {
		return nil, nil
	}

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
		// pos-weight 策略在权重调整后再过滤 minScore
		if !needsPosWeight && r.Score < minScore {
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

	// 方案3: POS 权重后处理
	if needsPosWeight {
		hits = my.applyPosWeight(query, hits)
		if minScore > 0 {
			var filtered []formatter.SearchHit
			for _, h := range hits {
				if h.Score >= minScore {
					filtered = append(filtered, h)
				}
			}
			hits = filtered
		}
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

// isNounPos 判断是否为名词性标签
// CJK 标签: n, ng, nr, ns, nt, nz, vn, an, nrt, nrl, nrfg
// English 标签: n
func isNounPos(pos string) bool {
	switch pos {
	case "n", "ng", "nr", "ns", "nt", "nz", "vn", "an", "nrt", "nrl", "nrfg":
		return true
	}
	return false
}

// isContentPos 判断是否为实词标签（动词、形容词、副词、未知词）
// CJK 标签: v, vd, vi, vg, vq, a, ag, ad
// English 标签: v, adj, adv
func isContentPos(pos string) bool {
	switch pos {
	case "v", "vd", "vi", "vg", "vq", "adj", "adv", "a", "ag", "ad", "x":
		return true
	}
	return false
}

// extractPosTerms 提取查询中所有名词和实词（动词/形容词等），过滤单字和停用词
func (my *Searcher) extractPosTerms(query string) (nouns, verbs []string) {
	posTokens := my.tokenizer.Pos(query)
	for _, pt := range posTokens {
		if pt.Text == "" {
			continue
		}
		safe := ftsSafeRe.ReplaceAllString(pt.Text, " ")
		word := strings.TrimSpace(safe)
		if word == "" || utf8.RuneCountInString(word) <= 1 {
			continue
		}
		if isNounPos(pt.Pos) {
			nouns = append(nouns, word)
		} else if isContentPos(pt.Pos) {
			verbs = append(verbs, word)
		}
	}
	return
}

// buildFTSQueryPosMust 方案1: 名词 MUST (AND) + 动词/形容词 SHOULD (OR)
// 生成 FTS5 enhanced query: noun1 AND noun2 AND (verb1 OR verb2)
// FTS5 不支持 +prefix MUST 语法（那是 FTS3/4 的），用显式 AND/OR 分组实现
// 实验性策略 — 不推荐使用：pos-must 对 300-rune chunk 过严，召回率低
func (my *Searcher) buildFTSQueryPosMust(query string) string {
	nouns, verbs := my.extractPosTerms(query)
	if len(nouns) == 0 && len(verbs) == 0 {
		return buildFTSQuery(query)
	}
	mustTerms := nouns
	shouldTerms := verbs
	// 截断：总词数不超过 256
	total := len(mustTerms) + len(shouldTerms)
	if total > 256 {
		ratio := float64(256) / float64(total)
		mustCap := int(float64(len(mustTerms)) * ratio)
		shouldCap := 256 - mustCap
		if mustCap > len(mustTerms) {
			mustCap = len(mustTerms)
			shouldCap = 256 - mustCap
		}
		if shouldCap > len(shouldTerms) {
			shouldCap = len(shouldTerms)
		}
		mustTerms = mustTerms[:mustCap]
		shouldTerms = shouldTerms[:shouldCap]
	}
	if len(mustTerms) == 0 {
		return strings.Join(shouldTerms, " OR ")
	}
	if len(shouldTerms) == 0 {
		return strings.Join(mustTerms, " AND ")
	}
	must := strings.Join(mustTerms, " AND ")
	should := "(" + strings.Join(shouldTerms, " OR ") + ")"
	return must + " AND " + should
}

// buildFTSQueryPosOr 方案2: POS 过滤（只保留名词+动词+形容词）+ OR
func (my *Searcher) buildFTSQueryPosOr(query string) string {
	nouns, verbs := my.extractPosTerms(query)
	terms := append(nouns, verbs...)
	if len(terms) == 0 {
		return buildFTSQuery(query)
	}
	if len(terms) > 256 {
		terms = terms[:256]
	}
	return strings.Join(terms, " OR ")
}

// nounWeight 名词命中加分（方案3：pos-weight 策略）
// contentWeight 动词/形容词命中加分
// contentWeight 小于 nounWeight，体现不同词性的重要性差异
const nounWeight = 0.05
const contentWeight = 0.02

// applyPosWeight 方案3: 对搜索结果按 POS 权重重排（实验性 — 效果不明显，不推荐）
// 名词在 chunk 中出现得越多，分数越高；动词/形容词较弱的加分
func (my *Searcher) applyPosWeight(query string, hits []formatter.SearchHit) []formatter.SearchHit {
	if len(hits) == 0 {
		return hits
	}
	posTokens := my.tokenizer.Pos(query)
	if len(posTokens) == 0 {
		return hits
	}
	var nounTerms, contentTerms []string
	for _, pt := range posTokens {
		word := strings.ToLower(pt.Text)
		if word == "" {
			continue
		}
		if isNounPos(pt.Pos) {
			nounTerms = append(nounTerms, word)
		} else if isContentPos(pt.Pos) {
			contentTerms = append(contentTerms, word)
		}
	}
	for i := range hits {
		bonus := 0.0
		snippet := strings.ToLower(hits[i].Snippet)
		for _, nt := range nounTerms {
			if strings.Contains(snippet, nt) {
				bonus += nounWeight
			}
		}
		for _, ct := range contentTerms {
			if strings.Contains(snippet, ct) {
				bonus += contentWeight
			}
		}
		hits[i].Score += bonus
		if hits[i].Score > 1.0 {
			hits[i].Score = 1.0
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})
	return hits
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
