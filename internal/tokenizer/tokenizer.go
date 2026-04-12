package tokenizer

type Tokenizer interface {
	Cut(text string) []string
	CutForSearch(text string) []string
	TokenizeToString(text string) string
}
