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
	// Initialize Docker client with default configuration
	docker, err := ichiran.NewDocker()
	if err != nil {
		panic(err)
	}
	defer docker.Close()

	// Initialize the environment (downloads, builds and starts containers if they are not running)
	if err := docker.Init(); err != nil {
		panic(err)
	}

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
	fmt.Printf("Gloss: %#v\n",		tokens.Gloss())
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
Gloss: "私(I; me) は(indicates sentence topic; indicates contrast with another option (stated or unstated); adds emphasis) 日本語(Japanese (language)) を(indicates direct object of action; indicates subject of causative expression; indicates an area traversed; indicates time (period) over which action takes place; indicates point of departure or separation of action; indicates object of desire, like, hate, etc.) . "
GlossParts: []string{"私(I; me)",
	"は(indicates sentence topic; indicates contrast with another option (stated or unstated); adds emphasis)",
	"日本語(Japanese (language))",
	"を(indicates direct object of action; indicates subject of causative expression; indicates an area traversed; indicates time (period) over which action takes place; indicates point of departure or separation of action; indicates object of desire, like, hate, etc.)",
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
> **The Docker library in Go is not standalone - it requires a running Docker daemon: Docker Desktop (Windows/Mac) or Docker Engine (Linux)** must be installed and running for this library to work.**
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
