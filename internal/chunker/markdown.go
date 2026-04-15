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
var base64ImgRe = regexp.MustCompile(`!\[.*?\]\(data:image/`)

func (my *MarkdownChunker) Chunk(title string, body string) ([]Chunk, error) {
	if body == "" {
		return nil, nil
	}

	body = my.stripBase64Images(body)

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

		lineTokens := estimateTokens(line)

		if current.Len() > 0 {
			hardLimit := estTokens+lineTokens > my.MaxTokens*2
			score := my.breakScore(trimmed)
			softLimit := score >= 80 && estTokens > 0

			if hardLimit || softLimit {
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
		estTokens += lineTokens
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

func (my *MarkdownChunker) breakScore(line string) int {
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
	ascii := 0
	cjk := 0
	for _, r := range text {
		if r < 128 {
			ascii++
		} else {
			cjk++
		}
	}
	return ascii/4 + cjk*2
}

func (my *MarkdownChunker) stripBase64Images(body string) string {
	return base64ImgRe.ReplaceAllStringFunc(body, func(match string) string {
		idx := strings.Index(match, ";base64,")
		if idx < 0 {
			return match
		}
		return match[:idx+len(";base64,")] + "...(truncated)"
	})
}
