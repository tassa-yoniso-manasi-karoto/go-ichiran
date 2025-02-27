
package ichiran

import (
	"encoding/csv"
	"fmt"
	"os"
	"io"
	"strings"
	"strconv"

	"github.com/gookit/color"
	"github.com/k0kubun/pp"
	"github.com/tidwall/pretty"
)

// KanjiFrequencyMap maps kanji characters to their frequency rank
type KanjiFrequencyMap map[string]int



func main() {
	freqMap, err := LoadKanjiFrequencyData("heisig-kanjis.csv")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	// Step 1: Find the maximum index in the map
	maxIndex := -1
	for _, index := range freqMap {
		if index > maxIndex {
			maxIndex = index
		}
	}

	// Step 2: Create a slice with the required size
	result := make([]string, maxIndex+1)

	// Step 3: Populate the slice with values from the map
	for kanji, index := range freqMap {
		result[index-1] = kanji
	}
	pp.BufferFoldThreshold = 100000
	pp.Println(result)
}


// LoadKanjiFrequencyData loads kanji frequency data from a CSV file
func LoadKanjiFrequencyData(csvPath string) (KanjiFrequencyMap, error) {
	file, err := os.Open(csvPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// Skip header
	if _, err := reader.Read(); err != nil {
		return nil, fmt.Errorf("failed to read CSV header: %w", err)
	}

	freqMap := make(map[string]int)
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV record: %w", err)
		}

		// Parse readings (columns 6 and 7 in the CSV)
		onReadings := strings.Split(strings.TrimSpace(record[6]), ";")
		kunReadings := strings.Split(strings.TrimSpace(record[7]), ";")
		
		// Combine all readings
		var readings []string
		readings = append(readings, onReadings...)
		readings = append(readings, kunReadings...)

		// Clean up readings
		for i, reading := range readings {
			readings[i] = strings.TrimSpace(reading)
		}
		rank, err := strconv.Atoi(record[2])
		if err != nil {
			continue
		}
		
		freqMap[record[0]] = rank
	}

	return freqMap, nil
}



func placeholder3456() {
	fmt.Println("")
	pretty.Pretty([]byte{})
	color.Redln(" 𝒻*** 𝓎ℴ𝓊 𝒸ℴ𝓂𝓅𝒾𝓁ℯ𝓇")
	pp.Println("𝓯*** 𝔂𝓸𝓾 𝓬𝓸𝓶𝓹𝓲𝓵𝓮𝓻")
}

