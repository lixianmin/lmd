package chunker

import (
	"regexp"
	"strings"
)

type MarkdownChunker struct {
	MaxTokens int
}

func NewMarkdownChunker(maxTokens int) *MarkdownChunker {
	if maxTokens <= 0 {
		maxTokens = 900
	}
	return &MarkdownChunker{MaxTokens: maxTokens}
}

var headingRe = regexp.MustCompile(`^(#{1,6})\s+`)
var codeFenceRe = regexp.MustCompile("^```")
var hrRe = regexp.MustCompile(`^(-{3,}|\*{3,}|_{3,})$`)

func (c *MarkdownChunker) Chunk(title string, body string) ([]Chunk, error) {
	if body == "" {
		return nil, nil
	}

	lines := strings.Split(body, "\n")
	var chunks []Chunk
	var current strings.Builder
	currentStart := 0
	inCodeBlock := false
	estTokens := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if codeFenceRe.MatchString(trimmed) {
			inCodeBlock = !inCodeBlock
		}

		if !inCodeBlock && current.Len() > 0 {
			score := c.breakScore(trimmed)
			forceFlush := estTokens > c.MaxTokens*2
			softFlush := estTokens > c.MaxTokens*2/3 && score > 0
			lineBreak := forceFlush && trimmed == ""

			if softFlush || lineBreak {
				content := current.String()
				chunks = append(chunks, Chunk{
					Content:    strings.TrimSpace(content),
					Position:   currentStart,
					TokenCount: estTokens,
				})
				current.Reset()
				currentStart = i
				estTokens = 0
			} else if forceFlush {
				content := current.String()
				chunks = append(chunks, Chunk{
					Content:    strings.TrimSpace(content),
					Position:   currentStart,
					TokenCount: estTokens,
				})
				current.Reset()
				currentStart = i
				estTokens = 0
			}
		}

		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
		estTokens += estimateTokens(line)
	}

	if current.Len() > 0 {
		content := current.String()
		chunks = append(chunks, Chunk{
			Content:    strings.TrimSpace(content),
			Position:   currentStart,
			TokenCount: estTokens,
		})
	}

	return chunks, nil
}

func (c *MarkdownChunker) breakScore(line string) int {
	if headingRe.MatchString(line) {
		level := len(regexp.MustCompile(`^#+`).FindString(line))
		switch {
		case level == 1:
			return 100
		case level == 2:
			return 90
		case level == 3:
			return 80
		default:
			return 70
		}
	}
	if hrRe.MatchString(line) {
		return 60
	}
	if line == "" {
		return 20
	}
	return 0
}

func estimateTokens(text string) int {
	return len(text) / 2
}
