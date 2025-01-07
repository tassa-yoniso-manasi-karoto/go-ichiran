### Status: alpha [![Go Reference](https://pkg.go.dev/badge/github.com/tassa-yoniso-manasi-karoto/go-ichiran.svg)](https://pkg.go.dev/github.com/tassa-yoniso-manasi-karoto/go-ichiran) [![Go Report Card](https://goreportcard.com/badge/github.com/tassa-yoniso-manasi-karoto/go-ichiran)](https://goreportcard.com/report/github.com/tassa-yoniso-manasi-karoto/go-ichiran)

A Go library for Japanese text analysis using the [Ichiran](https://github.com/tshatrov/ichiran) morphological analyzer in Docker compose containers. This client provides easy access to Japanese language parsing, including readings, translations, and grammatical analysis.

## Features

-  Morphological analysis of Japanese text
-  Kanji readings and translations
-  Romaji (romanization) support
-  Part-of-speech tagging
-  Conjugation analysis
-  Docker-based deployment
-  LLM-generated README

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

	// Initialize the environment (downloads and starts containers if needed)
	// NOTE: if you have 'exec: "ichiran-cli": executable file not found' errors,
	// use docker.InitForce() to bypass cache and force rebuild from scratch.
	if err := docker.Init(); err != nil {
		panic(err)
	}

	// Create an Ichiran client
	client, err := ichiran.NewClient(ichiran.DefaultConfig())
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Analyze Japanese text
	tokens, err := client.Analyze("私は日本語を勉強しています。")
	if err != nil {
		panic(err)
	}

	// Print different representations
	fmt.Println("Tokenized:", tokens.TokenizedStr())
	fmt.Println("Kana:", tokens.Kana())
	fmt.Println("Romaji:", tokens.Roman())
	fmt.Println("Gloss:", tokens.GlossString())
}
```

### Output

```
Tokenized: 私 は 日本語 を 勉強しています . 
Kana: わたし ‌は にほんご を べんきょう しています . 
Romaji: watashi wa nihongo wo benkyō shiteimasu . 
Gloss: 私(I; me) は(indicates sentence topic; indicates contrast with another option (stated or unstated); adds emphasis) 日本語(Japanese (language)) を(indicates direct object of action; indicates subject of causative expression; indicates an area traversed; indicates time (period) over which action takes place; indicates point of departure or separation of action; indicates object of desire, like, hate, etc.) . 
```
## Requirements

**Note: The Docker library in Go is not standalone - it requires a running Docker daemon. Docker Desktop (Windows/Mac) or Docker Engine (Linux) must be installed and running for this library to work.**

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

## License

GPL3
