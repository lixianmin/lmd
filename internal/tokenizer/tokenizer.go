package tokenizer

type SegPos struct {
	Text, Pos string
}

type Tokenizer interface {
	Cut(text string) []string
	CutForSearch(text string) []string
	TokenizeToString(text string) string
	Pos(text string) []SegPos
}
