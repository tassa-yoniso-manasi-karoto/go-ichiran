### Status: alpha [![Go Reference](https://pkg.go.dev/badge/github.com/tassa-yoniso-manasi-karoto/go-ichiran.svg)](https://pkg.go.dev/github.com/tassa-yoniso-manasi-karoto/go-ichiran) [![Go Report Card](https://goreportcard.com/badge/github.com/tassa-yoniso-manasi-karoto/go-ichiran)](https://goreportcard.com/report/github.com/tassa-yoniso-manasi-karoto/go-ichiran)

A Go library for Japanese text analysis using the [Ichiran](https://github.com/tshatrov/ichiran) morphological analyzer in Docker compose containers. This client provides easy access to Japanese language parsing, including readings, translations, and grammatical analysis.

## Features

-  **Morphological analysis** of Japanese text
-  **Kanji readings** and translations
-  **Romaji** (romanization) support
-  🆕 **Selective transliteration**: performs selective transliteration of text based on kanji frequency and phonetic regularity. Kanji with a frequency rank below the specified frequency threshold and regular readings are preserved, while others are converted to hiragana.
-  Part-of-speech tagging
-  Conjugation analysis
-  **Download & manage the docker containers automatically using the Docker Compose Go API** 🚀

## Installation

```bash
go get github.com/tassa-yoniso-manasi-karoto/go-ichiran
```

## Quick Start

### Package-Level Functions (Simple Usage)

```go
import (
	"context"
	"fmt"
	"log"

	"github.com/tassa-yoniso-manasi-karoto/go-ichiran"
)

func main() {
	// Create a context
	ctx := context.Background()
	
	// Initialize the environment (downloads, builds and starts containers)
	if err := ichiran.Init(ctx); err != nil {
		log.Fatal(err)
	}
	defer ichiran.Close()

	// Analyze Japanese text
	tokens, err := ichiran.Analyze(ctx, "私は日本語を勉強しています")
	if err != nil {
		log.Fatal(err)
	}
	
	// Selective transliteration: preserve only the top 1000 most frequent kanji
	tlit, err := tokens.SelectiveTranslit(1000)
	if err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("Tokenized: %#v\n", tokens.Tokenized())
	fmt.Printf("Kana: %#v\n", tokens.Kana())
	fmt.Printf("Roman: %#v\n", tokens.Roman())
	fmt.Printf("SelectiveTranslit: %#v\n", tlit)
}
```

### Manager-Based API (Multiple Instances)

```go
import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tassa-yoniso-manasi-karoto/go-ichiran"
)

func main() {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	
	// Create a custom manager with options
	manager, err := ichiran.NewManager(ctx, 
		ichiran.WithProjectName("ichiran-custom"),
		ichiran.WithQueryTimeout(1*time.Minute))
	if err != nil {
		log.Fatal(err)
	}
	
	// Initialize the environment
	if err := manager.Init(ctx); err != nil {
		log.Fatal(err)
	}
	defer manager.Close()

	// Analyze Japanese text using the custom manager
	tokens, err := manager.Analyze(ctx, "私は日本語を勉強しています")
	if err != nil {
		log.Fatal(err)
	}
	
	// Process the results
	fmt.Printf("Tokenized: %#v\n", tokens.Tokenized())
	fmt.Printf("Kana: %#v\n", tokens.Kana())
	fmt.Printf("Roman: %#v\n", tokens.Roman())
	
	// Run a second manager instance with different settings
	manager2, err := ichiran.NewManager(ctx, 
		ichiran.WithProjectName("ichiran-second"),
		ichiran.WithContainerName("ichiran-second-main-1"))
	if err != nil {
		log.Fatal(err)
	}
	
	if err := manager2.Init(ctx); err != nil {
		log.Fatal(err)
	}
	defer manager2.Close()
	
	// Use the second manager instance
	tokens2, err := manager2.Analyze(ctx, "さようなら")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Second manager result: %#v\n", tokens2.Roman())
}
```
 
### Output

```
Tokenized:	"私 は 日本語 を 勉強しています"
TokenizedParts:		[]string{"私", "は", "日本語", "を", "勉強しています"}

Kana:		"わたし は にほんご を べんきょう しています"
KanaParts:		[]string{"わたし", "は", "にほんご", "を", "べんきょう しています"}

Roman:		"watashi wa nihongo wo benkyō shiteimasu"
RomanParts:		[]string{"watashi", "wa", "nihongo", "wo", "benkyō shiteimasu"}

SelectiveTranslit: "私は日本語をべんきょう"

GlossParts: []string{"私(I; me)",
	"は (indicates sentence topic; indicates contrast with another option (stated or unstated); adds emphasis)",
	"日本語 (Japanese (language))",
	"を (indicates direct object of action; indicates subject of causative expression; indicates an area traversed; indicates time (period) over which action takes place; indicates point of departure or separation of action; indicates object of desire, like, hate, etc.)",
	"勉強 (study; diligence; working hard; experience; lesson (for the future); discount; price reduction)",
	"して (to do; to carry out; to perform; to cause to become; to make (into); to turn (into); to serve as; to act as; to work as; to wear (clothes, a facial expression, etc.); to judge as being; to view as being; to think of as; to treat as; to use as; to decide on; to choose; to be sensed (of a smell, noise, etc.); to be (in a state, condition, etc.); to be worth; to cost; to pass (of time); to elapse; to place, or raise, person A to a post or status B; to transform A to B; to make A into B; to exchange A for B; to make use of A for B; to view A as B; to handle A as if it were B; to feel A about B; verbalizing suffix (applies to nouns noted in this dictionary with the part of speech \"vs\"); creates a humble verb (after a noun prefixed with \"o\" or \"go\"); to be just about to; to be just starting to; to try to; to attempt to)",
	"います (to be (of animate objects); to exist; to stay; to be ...-ing; to have been ...-ing)"}
```


> [!TIP]
> if you have 'exec: "ichiran-cli": executable file not found' errors, remove directory ./docker/pgdata (as recommended by README of ichiran repo) at location below and use `InitRecreate(ctx, true)` to bypass cache and force rebuild from scratch.

> [!NOTE]
> Because ichiran doesn't retain spaces during its tokenization, the methods Roman, Kana, Tokenized are simply wrappers around RomanParts, KanaParts, TokenizedParts that joins their parts indiscriminately with a space.<br> For a more robust implementation that worksaround this limitation, use go-ichiran through [translitkit](https://github.com/tassa-yoniso-manasi-karoto/translitkit).

## Docker compose containers' location

- Linux: ~/.config/ichiran
- macOS: ~/Library/Application Support/ichiran
- Windows: %LOCALAPPDATA%\ichiran

## Requirements


> [!IMPORTANT]
> **The Docker library in Go is not standalone - it requires a running Docker daemon: Docker Desktop (Windows/Mac) or Docker Engine (Linux)** must be installed and running for this library to work.
### Windows
1. **Docker Desktop for Windows**
   - Download and install from [Docker Hub](https://hub.docker.com/editions/community/docker-ce-desktop-windows)
   - Requires Windows 10/11 Pro, Enterprise, or Education (64-bit)
   - WSL 2 backend is recommended
   - Hardware requirements:
     - 64-bit processor with Second Level Address Translation (SLAT)
     - 4GB system RAM
     - BIOS-level hardware virtualization support must be enabled

2. **WSL 2 (Windows Subsystem for Linux)**
   - Required for best performance
   - Install via PowerShell (as administrator):
     ```powershell
     wsl --install
     ```
   - Restart your computer after installation

3. **System Requirements**
   - Go 1.19 or later
   - Internet connection (for initial setup)

### macOS
1. **Docker Desktop for Mac**
   - Download and install from [Docker Hub](https://hub.docker.com/editions/community/docker-ce-desktop-mac)
   - Compatible with macOS 10.15 or newer

2. **System Requirements**
   - Go 1.19 or later
   - Internet connection (for initial setup)

### Linux
1. **Docker Engine**
   - Install using your distribution's package manager
   - Docker Compose V2 (included with recent Docker Engine installations)
   ```bash
   # Ubuntu/Debian
   sudo apt-get update
   sudo apt-get install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
   ```

2. **System Requirements**
   - Go 1.19 or later
   - Internet connection (for initial setup)

### Post-Installation
1. Verify Docker installation:
   ```bash
   docker --version
   docker compose version
   ```

2. Start Docker service (if not started):
   ```bash
   # Windows/Mac: Start Docker Desktop
   # Linux:
   sudo systemctl start docker
   ```

3. (Optional) Configure non-root access on Linux:
   ```bash
   sudo usermod -aG docker $USER
   # Log out and back in for changes to take effect
   ```

## Alternatives

- [ikawaha/kagome](https://github.com/ikawaha/kagome): self-contained Japanese Morphological Analyzer written in pure Go
- [shogo82148/go-mecab](https://github.com/shogo82148/go-mecab): MeCab binding for Golang

## License

GPL3