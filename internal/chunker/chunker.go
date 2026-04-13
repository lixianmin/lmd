package chunker

type Chunk struct {
	Content    string
	Position   int
	TokenCount int
}

type Chunker interface {
	Chunk(title string, body string) ([]Chunk, error)
}
