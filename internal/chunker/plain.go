package chunker

import (
	"strings"
	"unicode/utf8"
)

type PlainTextChunker struct {
	chunkSize    int
	hardMax      int
	overlapChars int
	minChunkSize int
}

func NewPlainTextChunker(chunkSize int) *PlainTextChunker {
	if chunkSize <= 0 {
		chunkSize = 300
	}
	overlapChars := max(chunkSize*overlapPercent/100, minOverlapChars)
	return &PlainTextChunker{
		chunkSize:    chunkSize,
		hardMax:      chunkSize + chunkSize/2,
		overlapChars: overlapChars,
		minChunkSize: chunkSize / 4,
	}
}

func (my *PlainTextChunker) Chunk(body string) ([]Chunk, error) {
	if body == "" {
		return nil, nil
	}

	runes := []rune(body)
	totalRunes := len(runes)

	if totalRunes <= my.chunkSize {
		return []Chunk{{
			Content:    strings.TrimSpace(body),
			StartLine:  0,
			EndLine:    0,
			TokenCount: estimateTokens(body),
		}}, nil
	}

	lineStarts := collectLineStarts(body)

	var rawChunks []rawChunk
	pos := 0

	for pos < totalRunes {
		remaining := totalRunes - pos
		if remaining <= my.chunkSize {
			content := string(runes[pos:])
			rawChunks = append(rawChunks, rawChunk{
				content: content,
				start:   len(string(runes[:pos])),
				end:     len(body),
			})
			break
		}

		targetPos := pos + my.chunkSize
		cutPos := my.findSentenceBoundary(runes, targetPos)
		if cutPos <= pos {
			cutPos = targetPos
		}

		content := string(runes[pos:cutPos])
		rawChunks = append(rawChunks, rawChunk{
			content: content,
			start:   len(string(runes[:pos])),
			end:     len(string(runes[:cutPos])),
		})
		pos = cutPos
	}

	rawChunks = my.mergeSmallTail(rawChunks)
	chunks := my.buildChunks(rawChunks, lineStarts)
	chunks = my.addOverlap(chunks)
	return chunks, nil
}

func (my *PlainTextChunker) findSentenceBoundary(runes []rune, targetPos int) int {
	searchRadius := overlapWindowPad
	searchStart := max(targetPos-searchRadius, 0)
	searchEnd := min(targetPos+searchRadius, len(runes))

	bestPos := -1
	bestDist := len(runes)

	for i := searchStart; i < searchEnd; i++ {
		r := runes[i]
		if r == '。' || r == '！' || r == '？' || r == '.' || r == '!' || r == '?' || r == '\n' {
			cutPoint := i + 1
			dist := abs(cutPoint - targetPos)
			if dist < bestDist {
				bestDist = dist
				bestPos = cutPoint
			}
		}
	}

	if bestPos <= 0 {
		return targetPos
	}
	return bestPos
}

func (my *PlainTextChunker) mergeSmallTail(rawChunks []rawChunk) []rawChunk {
	if len(rawChunks) <= 1 {
		return rawChunks
	}

	last := rawChunks[len(rawChunks)-1]
	if utf8.RuneCountInString(last.content) >= my.minChunkSize {
		return rawChunks
	}

	prev := rawChunks[len(rawChunks)-2]
	combined := prev.content + "\n" + last.content
	if utf8.RuneCountInString(combined) <= my.hardMax {
		rawChunks[len(rawChunks)-2] = rawChunk{
			content: combined,
			start:   prev.start,
			end:     last.end,
		}
		return rawChunks[:len(rawChunks)-1]
	}

	return rawChunks
}

func (my *PlainTextChunker) buildChunks(rawChunks []rawChunk, lineStarts []int) []Chunk {
	chunks := make([]Chunk, 0, len(rawChunks))
	for _, rc := range rawChunks {
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

func (my *PlainTextChunker) addOverlap(chunks []Chunk) []Chunk {
	if len(chunks) <= 1 {
		return chunks
	}

	result := make([]Chunk, len(chunks))
	copy(result, chunks)

	for i := 1; i < len(chunks); i++ {
		prev := chunks[i-1].Content
		prevRunes := []rune(prev)

		if len(prevRunes) <= my.overlapChars {
			continue
		}

		windowStart := len(prevRunes) - my.overlapChars - overlapWindowPad
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd := len(prevRunes) - my.overlapChars + overlapWindowPad
		if windowEnd > len(prevRunes) {
			windowEnd = len(prevRunes)
		}

		idealIdx := len(prevRunes) - windowStart - my.overlapChars
		windowRunes := prevRunes[windowStart:windowEnd]

		var locations []int
		for ri, r := range windowRunes {
			if r == '。' || r == '！' || r == '？' || r == '.' || r == '!' || r == '?' || r == '\n' {
				locations = append(locations, ri)
			}
		}

		if len(locations) == 0 {
			continue
		}

		bestIdx := locations[0]
		bestDist := abs(bestIdx - idealIdx)
		for _, loc := range locations {
			dist := abs(loc - idealIdx)
			if dist < bestDist {
				bestDist = dist
				bestIdx = loc
			}
		}

		snapPos := windowStart + bestIdx + 1
		if snapPos < 0 {
			snapPos = 0
		}
		if snapPos > len(prevRunes) {
			snapPos = len(prevRunes)
		}

		overlapText := strings.TrimSpace(string(prevRunes[snapPos:]))
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
