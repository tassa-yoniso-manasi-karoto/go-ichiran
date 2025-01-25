### Status: alpha [![Go Reference](https://pkg.go.dev/badge/github.com/tassa-yoniso-manasi-karoto/go-ichiran.svg)](https://pkg.go.dev/github.com/tassa-yoniso-manasi-karoto/go-ichiran) [![Go Report Card](https://goreportcard.com/badge/github.com/tassa-yoniso-manasi-karoto/go-ichiran)](https://goreportcard.com/report/github.com/tassa-yoniso-manasi-karoto/go-ichiran)

A Go library for Japanese text analysis using the [Ichiran](https://github.com/tshatrov/ichiran) morphological analyzer in Docker compose containers. This client provides easy access to Japanese language parsing, including readings, translations, and grammatical analysis.

## Features

-  Morphological analysis of Japanese text
-  Kanji readings and translations
-  Romaji (romanization) support
-  Part-of-speech tagging
-  Conjugation analysis
-  Download & manage the docker containers directly using the Docker Compose Go API

## Installation

```bash
go get github.com/tassa-yoniso-manasi-karoto/go-ichiran
```

## tldr

```go
func main() {
	// Initialize the environment (downloads, builds and starts containers if they are not running)
	ichiran.MustInit()
	defer ichiran.Close()

	tokens, err := ichiran.Analyze("私は日本語を勉強しています。")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Tokenized: %#v\n",		tokens.Tokenized())
	fmt.Printf("TokenizedParts: %#v\n",	tokens.TokenizedParts())
	fmt.Printf("Kana: %#v\n",		tokens.Kana())
	fmt.Printf("KanaParts: %#v\n",		tokens.KanaParts())
	fmt.Printf("Roman: %#v\n",		tokens.Roman())
	fmt.Printf("RomanParts: %#v\n",		tokens.RomanParts())
	fmt.Printf("GlossParts: %#v\n",		tokens.GlossParts())
}
```
 
### Output

```
Tokenized: "私 は 日本語 を 勉強しています . "
TokenizedParts: []string{"私", "は", "日本語", "を", "勉強しています", ". "}
Kana: "わたし は にほんご を べんきょう しています . "
KanaParts: []string{"わたし", "は", "にほんご", "を", "べんきょう しています", ". "}
Roman: "watashi wa nihongo wo benkyō shiteimasu . "
RomanParts: []string{"watashi", "wa", "nihongo", "wo", "benkyō shiteimasu", ". "}
GlossParts: []string{"私(I; me)",
	"は (indicates sentence topic; indicates contrast with another option (stated or unstated); adds emphasis)",
	"日本語 (Japanese (language))",
	"を (indicates direct object of action; indicates subject of causative expression; indicates an area traversed; indicates time (period) over which action takes place; indicates point of departure or separation of action; indicates object of desire, like, hate, etc.)",
	"勉強 (study; diligence; working hard; experience; lesson (for the future); discount; price reduction)",
	"して (to do; to carry out; to perform; to cause to become; to make (into); to turn (into); to serve as; to act as; to work as; to wear (clothes, a facial expression, etc.); to judge as being; to view as being; to think of as; to treat as; to use as; to decide on; to choose; to be sensed (of a smell, noise, etc.); to be (in a state, condition, etc.); to be worth; to cost; to pass (of time); to elapse; to place, or raise, person A to a post or status B; to transform A to B; to make A into B; to exchange A for B; to make use of A for B; to view A as B; to handle A as if it were B; to feel A about B; verbalizing suffix (applies to nouns noted in this dictionary with the part of speech \"vs\"); creates a humble verb (after a noun prefixed with \"o\" or \"go\"); to be just about to; to be just starting to; to try to; to attempt to)",
	"います (to be (of animate objects); to exist; to stay; to be ...-ing; to have been ...-ing)",
	". "}
```


> [!TIP]
> if you have 'exec: "ichiran-cli": executable file not found' errors, remove directory ./docker/pgdata (as recommended by README of ichiran repo) at location below and use docker.InitForce() to bypass cache and force rebuild from scratch.

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
