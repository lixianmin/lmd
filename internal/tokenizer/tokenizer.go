package tokenizer

type SegPos struct {
	Text, Pos string
}

type Tokenizer interface {
	Cut(text string) []string
	CutForSearch(text string) []string
	TokenizeToString(text string) string
	GetIDF(word string) float64
	Pos(text string) []SegPos
}
