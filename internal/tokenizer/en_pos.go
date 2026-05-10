// English word→POS lookup table.
// Source: Brown Corpus (NLTK), ~1.16M tokens, 46,066 unique words.
// POS tags: n (noun), v (verb), adj (adjective), adv (adverb).
// To regenerate: run the Brown Corpus extraction script from brown_pos.json,
// then write each line as "word pos" to en_pos.txt.
package tokenizer

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed en_pos.txt
var enPosRaw []byte

var enPosOnce sync.Once
var enPosMap map[string]string

func loadEnPos() {
	enPosOnce.Do(func() {
		enPosMap = make(map[string]string, 50000)
		text := string(enPosRaw)
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				enPosMap[parts[0]] = parts[1]
			}
		}
	})
}
