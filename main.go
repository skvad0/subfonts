package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type GoogleFontsResponse struct {
	Items []struct {
		Family string            `json:"family"`
		Files  map[string]string `json:"files"`
	} `json:"items"`
}

var ollamaURL = "http://localhost:11434/api/generate"

type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Format string `json:"format"`
}

type OllamaResponse struct {
	Response string `json:"response"`
}

func main() {
	subFile := flag.String("sub", "", "Path to the subtitle file (.srt, .ass, etc.)")
	outDir := flag.String("out", "./fonts", "Directory to download the fonts into")
	apiKey := flag.String("key", "", "Google Fonts API Key")
	flag.Parse()

	if *subFile == "" {
		fmt.Println("Error: You must provide a subtitle file path using the -sub flag.")
		flag.Usage()
		os.Exit(1)
	}

	if *apiKey == "" {
		fmt.Println("Warning: No Google Fonts API key provided (-key). The tool will extract font names but cannot download them.")
	}

	fmt.Printf("Analyzing subtitle file: %s\n", *subFile)
	fonts, err := extractFonts(*subFile)
	if err != nil {
		fmt.Printf("Error extracting fonts: %v\n", err)
		os.Exit(1)
	}

	if len(fonts) == 0 {
		fmt.Println("No specific fonts found in the subtitle file. Standard system fonts likely used.")
		return
	}

	fmt.Printf("Found %d unique font(s): %s\n", len(fonts), strings.Join(fonts, ", "))

	if *apiKey != "" {
		err = resolveAndDownloadFonts(fonts, *outDir, *apiKey)
		if err != nil {
			fmt.Printf("Download error: %v\n", err)
		}
	}
}

func extractFonts(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(filePath))
	uniqueFonts := make(map[string]bool)

	switch ext {
	case ".ass", ".ssa":
		err = parseASS(file, uniqueFonts)
	default: // .srt and unknown formats
		err = parseSRT(file, uniqueFonts)
	}

	if err != nil {
		return nil, err
	}

	var fonts []string
	for font := range uniqueFonts {
		fonts = append(fonts, font)
	}
	return fonts, nil
}

func parseSRT(file *os.File, uniqueFonts map[string]bool) error {
	scanner := bufio.NewScanner(file)
	re := regexp.MustCompile(`(?i)<font[^>]+face=["'](.*?)["']`)

	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			if len(match) > 1 {
				uniqueFonts[strings.TrimSpace(match[1])] = true
			}
		}
	}
	return scanner.Err()
}

func parseASS(file *os.File, uniqueFonts map[string]bool) error {
	scanner := bufio.NewScanner(file)
	inStylesBlock := false
	fontNameIndex := 1

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inStylesBlock = strings.HasPrefix(line, "[V4+ Styles]") || strings.HasPrefix(line, "[V4 Styles]")
			continue
		}

		if inStylesBlock {
			if rest, ok := strings.CutPrefix(line, "Format:"); ok {
				for i, f := range strings.Split(rest, ",") {
					if strings.TrimSpace(f) == "Fontname" {
						fontNameIndex = i
						break
					}
				}
			} else if rest, ok := strings.CutPrefix(line, "Style:"); ok {
				values := strings.Split(rest, ",")
				if len(values) > fontNameIndex {
					uniqueFonts[strings.TrimSpace(values[fontNameIndex])] = true
				}
			}
		}
	}
	return scanner.Err()
}

func basicNormalize(name string) string {
	clean := strings.ToLower(name)
	clean = strings.ReplaceAll(clean, " ", "")
	clean = strings.ReplaceAll(clean, "-", "")
	clean = strings.ReplaceAll(clean, "_", "")
	return clean
}

func resolveAndDownloadFonts(requestedFonts []string, outDir string, apiKey string) error {
	fmt.Println("Fetching Google Fonts catalog...")
	catalog, err := fetchGoogleCatalog(apiKey)
	if err != nil {
		return err
	}

	matchedFonts := make(map[string]string)
	var missingFonts []string
	var failedToRetrieve []string

	for _, rawFont := range requestedFonts {
		lookupKey := basicNormalize(rawFont)

		if downloadURL, exists := catalog[lookupKey]; exists {
			fmt.Printf("Fast Match: '%s'\n", rawFont)
			matchedFonts[rawFont] = downloadURL
		} else {
			missingFonts = append(missingFonts, rawFont)
		}
	}

	if len(missingFonts) > 0 {
		fmt.Printf("\n%d font(s) missing from catalog lookup. Engaging AI Fallback...\n", len(missingFonts))

		aiCorrectedMap, aiErr := normalizeFontsWithAI(missingFonts)
		if aiErr != nil {
			fmt.Printf("AI Fallback failed: %v\n", aiErr)
			failedToRetrieve = append(failedToRetrieve, missingFonts...)
		} else {
			for _, rawName := range missingFonts {
				aiCleanName, ok := aiCorrectedMap[rawName]
				if !ok {
					aiCleanName = rawName
				}
				lookupKey := basicNormalize(aiCleanName)

				if downloadURL, exists := catalog[lookupKey]; exists {
					fmt.Printf("AI Rescued: '%s' -> Corrected to '%s'\n", rawName, aiCleanName)
					matchedFonts[aiCleanName] = downloadURL
				} else {
					failedToRetrieve = append(failedToRetrieve, rawName)
				}
			}
		}
	}

	fmt.Println("\nStarting Downloads...")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	for name, url := range matchedFonts {
		dest := filepath.Join(outDir, strings.ReplaceAll(name, " ", "_")+".ttf")
		if err := downloadFile(url, dest); err != nil {
			fmt.Printf("Error downloading '%s': %v\n", name, err)
		} else {
			fmt.Printf("Saved '%s'\n", name)
		}
	}

	if len(failedToRetrieve) > 0 {
		fmt.Printf("\n========================================\n")
		fmt.Printf("WARNING: UNRETRIEVED FONTS\n")
		fmt.Printf("========================================\n")
		fmt.Printf("The following fonts could not be located in the Google Fonts repository.\n")
		fmt.Printf("They may be paid, proprietary, or strictly system fonts:\n\n")
		for _, f := range failedToRetrieve {
			fmt.Printf("   - %s\n", f)
		}
		fmt.Printf("========================================\n")
	}

	return nil
}

func fetchGoogleCatalog(apiKey string) (map[string]string, error) {
	url := fmt.Sprintf("https://www.googleapis.com/webfonts/v1/webfonts?key=%s", apiKey)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var fontData GoogleFontsResponse
	if err := json.NewDecoder(resp.Body).Decode(&fontData); err != nil {
		return nil, err
	}

	catalog := make(map[string]string)
	for _, item := range fontData.Items {
		url := item.Files["regular"]
		if url == "" {
			for _, v := range item.Files {
				url = v
				break
			}
		}
		catalog[basicNormalize(item.Family)] = url
	}
	return catalog, nil
}

func normalizeFontsWithAI(missingFonts []string) (map[string]string, error) {
	missingJSON, _ := json.Marshal(missingFonts)

	prompt := fmt.Sprintf(`
	You are an expert typography resolution agent.
	The following array of font names failed to match in the Google Fonts catalog.
	They likely contain typos, appended weights (like "Bold" or "Italic"), or slight misnomers.
	
	Array to fix:
	%s
	
	Return a JSON dictionary mapping the exact provided raw name to the corrected official Google Font Family name.
	Strip out styles/weights (e.g. "Arial-Bold-MT" -> "Arial").
	
	CRITICAL RULE: Do NOT suggest alternatives or free equivalents for paid/proprietary fonts. 
	If a font is not a standard Google Font, return the exact original name provided.
	`, string(missingJSON))

	reqBody := OllamaRequest{
		Model:  "phi3",
		Prompt: prompt,
		Stream: false,
		Format: "json",
	}
	jsonData, _ := json.Marshal(reqBody)

	resp, err := http.Post(ollamaURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var llmResp OllamaResponse
	if err := json.Unmarshal(bodyBytes, &llmResp); err != nil {
		return nil, err
	}

	var corrected map[string]string
	if err := json.Unmarshal([]byte(llmResp.Response), &corrected); err != nil {
		return nil, err
	}

	return corrected, nil
}

func downloadFile(url string, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
