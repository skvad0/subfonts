# subfonts

A CLI tool that scans subtitle files (`.ass`, `.srt`) for font references, matches them against the Google Fonts catalog, and downloads the required font files automatically.

## The Problem

When you download a subtitle file for an anime, movie, or video, it often references specific fonts that need to be installed for the subtitles to render correctly. Hunting down each font manually is tedious — especially for `.ass` files that can reference a dozen different typefaces.

## How It Works

1. **Parse** — Extracts all font names from the subtitle file's style definitions and inline tags.
2. **Match** — Looks up each font against the Google Fonts API catalog using normalized name matching.
3. **AI Fallback** — Any fonts that fail the catalog lookup are sent to a local [Ollama](https://ollama.com/) instance (phi3 model), which corrects typos, strips weight suffixes (e.g. `Arial-Bold-MT` → `Arial`), and retries the lookup.
4. **Download** — Saves matched `.ttf` files into a local output directory.

## Usage

```bash
# Extract and download fonts
subfonts -sub "My Show S01E01.ass" -key YOUR_GOOGLE_FONTS_API_KEY

# Extract font names only (no download)
subfonts -sub "My Show S01E01.srt"

# Custom output directory
subfonts -sub input.ass -key YOUR_KEY -out ./my-fonts
```

### Flags

| Flag   | Default    | Description                          |
|--------|------------|--------------------------------------|
| `-sub` | *(required)* | Path to the subtitle file           |
| `-key` | *(none)*   | Google Fonts API key (enables download) |
| `-out` | `./fonts`  | Directory to save downloaded fonts   |

## Requirements

- [Go 1.21+](https://go.dev/dl/)
- A [Google Fonts API key](https://developers.google.com/fonts/docs/developer_api) (for downloading)
- [Ollama](https://ollama.com/) running locally with the `phi3` model pulled (for AI fallback)

```bash
# Pull the model used for AI fallback
ollama pull phi3
```

## Build

```bash
go build -o subfonts .
```

## Supported Formats

| Format | Detection Method |
|--------|-----------------|
| `.ass` / `.ssa` | `[V4+ Styles]` / `[V4 Styles]` `Fontname` field |
| `.srt` | `<font face="...">` inline HTML tags |
