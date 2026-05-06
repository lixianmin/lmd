package tokenizer

import (
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

//go:embed idf.txt
var idfRaw []byte

var idfOnce sync.Once
var idfMap map[string]float64

func loadIDF() {
	idfOnce.Do(func() {
		idfMap = make(map[string]float64, 350000)
		lines := strings.Split(string(idfRaw), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			if len(parts) != 2 {
				continue
			}
			v, err := strconv.ParseFloat(parts[1], 64)
			if err != nil {
				continue
			}
			idfMap[parts[0]] = v
		}
	})
}

func GetIDF(word string) float64 {
	loadIDF()
	return idfMap[word]
}
