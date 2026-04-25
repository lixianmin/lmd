package chunker

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	overlapPercent      = 15  // overlap 占 chunkSize 的百分比
	minOverlapChars     = 30  // overlap 最少 rune 数
	overlapWindowPad    = 50  // overlap 搜索窗口前后各留的 rune 数
	breakScoreH1        = 100 // H1 标题断点评分
	breakScoreH2        = 90  // H2 标题断点评分
	breakScoreH3        = 80  // H3 标题断点评分
	breakScoreH4        = 70  // H4 标题断点评分
	breakScoreCodeFence = 80  // 代码块边界断点评分
	breakScoreHR        = 60  // 水平线断点评分
	breakScoreBlank     = 20  // 空行断点评分
	breakPointDecay     = 0.7 // 断点评分的距离衰减系数
	asciiThreshold      = 128 // ASCII 字符分界
	tokensPerASCII      = 4   // 每 N 个 ASCII 字符 ≈ 1 token
	tokensPerCJK        = 2   // 每 1 个 CJK 字符 ≈ N tokens
)

var base64ImgRe = regexp.MustCompile(`!\[[^\]]*\]\(data:image/[^;]+;base64,[^)]*\)`)

type MarkdownChunker struct {
	chunkSize    int
	hardMax      int
	overlapChars int
	windowChars  int
	minChunkSize int
}

func NewMarkdownChunker(chunkSize int) *MarkdownChunker {
	if chunkSize <= 0 {
		chunkSize = 300
	}
	overlapChars := chunkSize * overlapPercent / 100
	if overlapChars < minOverlapChars {
		overlapChars = minOverlapChars
	}
	return &MarkdownChunker{
		chunkSize:    chunkSize,
		hardMax:      chunkSize + chunkSize/2,
		overlapChars: overlapChars,
		windowChars:  chunkSize / 2,
		minChunkSize: chunkSize / 4,
	}
}

type breakPoint struct {
	pos   int
	score int
}

type codeFence struct {
	start int
	end   int
}

func scanBreakPoints(text string) []breakPoint {
	h1Re := regexp.MustCompile(`\n#[^#]`)
	h2Re := regexp.MustCompile(`\n##[^#]`)
	h3Re := regexp.MustCompile(`\n###[^#]`)
	h4Re := regexp.MustCompile(`\n####[^#]`)
	codeFenceRe := regexp.MustCompile("\\n```[^\n]*")
	hrRe := regexp.MustCompile(`\n(?:---|\*\*\*|___)\s*\n`)
	blankRe := regexp.MustCompile(`\n\n+`)

	type pattern struct {
		re    *regexp.Regexp
		score int
	}
	patterns := []pattern{
		{h1Re, breakScoreH1},
		{h2Re, breakScoreH2},
		{h3Re, breakScoreH3},
		{h4Re, breakScoreH4},
		{codeFenceRe, breakScoreCodeFence},
		{hrRe, breakScoreHR},
		{blankRe, breakScoreBlank},
	}

	seen := make(map[int]int)
	for _, p := range patterns {
		locs := p.re.FindAllStringIndex(text, -1)
		for _, loc := range locs {
			pos := loc[0]
			if existing, ok := seen[pos]; !ok || p.score > existing {
				seen[pos] = p.score
			}
		}
	}

	points := make([]breakPoint, 0, len(seen))
	for pos, score := range seen {
		points = append(points, breakPoint{pos: pos, score: score})
	}
	sort.Slice(points, func(i, j int) bool {
		return points[i].pos < points[j].pos
	})
	return points
}

func scanCodeFences(text string) []codeFence {
	re := regexp.MustCompile("```")
	matches := re.FindAllStringIndex(text, -1)
	var fences []codeFence
	for i := 0; i < len(matches)-1; i += 2 {
		start := matches[i][0]
		end := matches[i+1][1]
		fences = append(fences, codeFence{start: start, end: end})
	}
	if len(matches)%2 == 1 {
		fences = append(fences, codeFence{start: matches[len(matches)-1][0], end: len(text)})
	}
	return fences
}

func isInsideCodeFence(pos int, fences []codeFence) bool {
	for _, f := range fences {
		if pos > f.start && pos < f.end {
			return true
		}
		if f.start > pos {
			break
		}
	}
	return false
}

func isCodeFenceBoundary(pos int, fences []codeFence) bool {
	for _, f := range fences {
		if pos == f.start {
			return true
		}
		if f.start > pos {
			break
		}
	}
	return false
}

type rawChunk struct {
	content string
	start   int
	end     int
}

func (my *MarkdownChunker) findBestCutoff(points []breakPoint, targetBytePos int, fences []codeFence) int {
	windowStart := targetBytePos - my.windowChars*4
	if windowStart < 0 {
		windowStart = 0
	}
	bestPos := targetBytePos
	bestScore := 0.0

	for _, bp := range points {
		if bp.pos < windowStart {
			continue
		}
		if bp.pos > targetBytePos {
			break
		}
		if isInsideCodeFence(bp.pos, fences) || isCodeFenceBoundary(bp.pos, fences) {
			continue
		}
		distance := float64(targetBytePos - bp.pos)
		windowBytes := float64(targetBytePos-windowStart) + 1
		normalizedDist := distance / windowBytes
		multiplier := 1.0 - (normalizedDist * normalizedDist * breakPointDecay)
		finalScore := float64(bp.score) * multiplier
		if finalScore > bestScore {
			bestScore = finalScore
			bestPos = bp.pos
		}
	}
	return bestPos
}

func byteOffsetToLine(offset int, lineStarts []int) int {
	lo, hi := 0, len(lineStarts)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		if lineStarts[mid] <= offset {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	return hi
}

func collectLineStarts(text string) []int {
	positions := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			positions = append(positions, i+1)
		}
	}
	return positions
}

func (my *MarkdownChunker) Chunk(body string) ([]Chunk, error) {
	if body == "" {
		return nil, nil
	}

	body = my.stripBase64Images(body)
	prepended := "\n" + body

	points := scanBreakPoints(prepended)
	fences := scanCodeFences(prepended)
	lineStarts := collectLineStarts(prepended)
	totalLen := len(prepended)

	var raw []rawChunk
	bytePos := 0

	for bytePos < totalLen {
		remainingRunes := utf8.RuneCountInString(prepended[bytePos:])

		if remainingRunes <= my.chunkSize {
			raw = append(raw, rawChunk{content: prepended[bytePos:], start: bytePos, end: totalLen})
			break
		}

		targetBytePos := bytePos
		runesNeeded := my.chunkSize
		for runesNeeded > 0 && targetBytePos < totalLen {
			_, size := utf8.DecodeRuneInString(prepended[targetBytePos:])
			targetBytePos += size
			runesNeeded--
		}

		if targetBytePos >= totalLen {
			raw = append(raw, rawChunk{content: prepended[bytePos:], start: bytePos, end: totalLen})
			break
		}

		bestBreak := my.findBestCutoff(points, targetBytePos, fences)
		if bestBreak <= bytePos {
			bestBreak = targetBytePos
		}

		raw = append(raw, rawChunk{content: prepended[bytePos:bestBreak], start: bytePos, end: bestBreak})
		bytePos = bestBreak
	}

	raw = my.hardSplit(raw, prepended)
	raw = my.mergeSmall(raw, prepended)
	chunks := my.buildChunks(raw, lineStarts)
	chunks = my.addOverlap(chunks)
	return chunks, nil
}

func (my *MarkdownChunker) stripBase64Images(body string) string {
	return base64ImgRe.ReplaceAllString(body, "...(truncated)")
}

func (my *MarkdownChunker) hardSplit(raw []rawChunk, source string) []rawChunk {
	var result []rawChunk
	for _, rc := range raw {
		runes := []rune(rc.content)
		if len(runes) <= my.hardMax {
			result = append(result, rc)
			continue
		}
		contentStart := rc.start
		byteOff := 0
		for i := 0; i < len(runes); i += my.hardMax {
			end := i + my.hardMax
			if end > len(runes) {
				end = len(runes)
			}
			partRunes := runes[i:end]
			partStr := string(partRunes)
			partLen := len(partStr)
			result = append(result, rawChunk{
				content: partStr,
				start:   contentStart + byteOff,
				end:     contentStart + byteOff + partLen,
			})
			byteOff += partLen
		}
	}
	return result
}

func (my *MarkdownChunker) mergeSmall(raw []rawChunk, source string) []rawChunk {
	if len(raw) <= 1 {
		return raw
	}

	var merged []rawChunk
	current := raw[0]

	for i := 1; i < len(raw); i++ {
		rc := raw[i]
		curRunes := utf8.RuneCountInString(current.content)
		rcRunes := utf8.RuneCountInString(rc.content)

		if curRunes < my.minChunkSize {
			combined := current.content + "\n" + rc.content
			if utf8.RuneCountInString(combined) <= my.hardMax {
				current = rawChunk{content: combined, start: current.start, end: rc.end}
				continue
			}
		}

		if rcRunes < my.minChunkSize && len(merged) == 0 && curRunes+1+rcRunes <= my.hardMax {
			combined := current.content + "\n" + rc.content
			if utf8.RuneCountInString(combined) <= my.hardMax {
				current = rawChunk{content: combined, start: current.start, end: rc.end}
				continue
			}
		}

		merged = append(merged, current)
		current = rc
	}
	merged = append(merged, current)

	if len(merged) > 1 && utf8.RuneCountInString(merged[0].content) < my.minChunkSize {
		combined := merged[0].content + "\n" + merged[1].content
		if utf8.RuneCountInString(combined) <= my.hardMax {
			merged[1] = rawChunk{content: combined, start: merged[0].start, end: merged[1].end}
			merged = merged[1:]
		}
	}

	return merged
}

func (my *MarkdownChunker) buildChunks(raw []rawChunk, lineStarts []int) []Chunk {
	chunks := make([]Chunk, 0, len(raw))
	for _, rc := range raw {
		content := strings.TrimSpace(rc.content)
		if content == "" {
			continue
		}
		startLine := byteOffsetToLine(rc.start, lineStarts)
		endLine := byteOffsetToLine(rc.end-1, lineStarts)
		if startLine > 0 {
			startLine--
		}
		if endLine > 0 {
			endLine--
		}
		chunks = append(chunks, Chunk{
			Content:    content,
			StartLine:  startLine,
			EndLine:    endLine,
			TokenCount: estimateTokens(content),
		})
	}
	return chunks
}

func (my *MarkdownChunker) addOverlap(chunks []Chunk) []Chunk {
	if len(chunks) <= 1 {
		return chunks
	}

	result := make([]Chunk, len(chunks))
	copy(result, chunks)

	for i := 1; i < len(chunks); i++ {
		prev := chunks[i-1].Content
		runes := []rune(prev)

		if len(runes) <= my.overlapChars {
			continue
		}

		windowStart := len(runes) - my.overlapChars - overlapWindowPad
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd := len(runes) - my.overlapChars + overlapWindowPad
		if windowEnd > len(runes) {
			windowEnd = len(runes)
		}

		windowRunes := runes[windowStart:windowEnd]
		idealIdx := len(runes) - windowStart - my.overlapChars

		var locations []int
		for ri, r := range windowRunes {
			if r == '。' || r == '！' || r == '？' || r == '.' || r == '!' || r == '?' {
				locations = append(locations, ri)
			}
		}

		if len(locations) == 0 {
			continue
		}

		bestRuneIdx := locations[0]
		bestDist := abs(bestRuneIdx - idealIdx)
		for _, loc := range locations {
			dist := abs(loc - idealIdx)
			if dist < bestDist {
				bestDist = dist
				bestRuneIdx = loc
			}
		}

		snapRunePos := windowStart + bestRuneIdx + 1
		if snapRunePos < 0 {
			snapRunePos = 0
		}
		if snapRunePos > len(runes) {
			snapRunePos = len(runes)
		}

		overlapText := strings.TrimSpace(string(runes[snapRunePos:]))
		if overlapText == "" {
			continue
		}

		newContent := overlapText + "\n\n" + result[i].Content
		if utf8.RuneCountInString(newContent) > my.hardMax {
			continue
		}
		result[i].Content = newContent
		result[i].TokenCount = estimateTokens(newContent)
	}

	return result
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func estimateTokens(text string) int {
	ascii := 0
	cjk := 0
	for _, r := range text {
		if r < asciiThreshold {
			ascii++
		} else {
			cjk++
		}
	}
	return ascii/tokensPerASCII + cjk*tokensPerCJK
}
