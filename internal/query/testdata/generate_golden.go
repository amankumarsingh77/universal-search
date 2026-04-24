//go:build ignore
// +build ignore

// generate_golden.go — one-shot dataset generator for the golden eval harness.
// Usage:
//
//	cd internal/query/testdata
//	GEMINI_API_KEY=<key> go run generate_golden.go -out golden_queries.raw.jsonl
//
// This file is excluded from normal builds by the //go:build ignore tag.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"
)

// retryGenerate calls GenerateContent with exponential backoff on 429/503.
func retryGenerate(ctx context.Context, client *genai.Client, model string, contents []*genai.Content, config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
	delays := []time.Duration{55 * time.Second, 65 * time.Second, 90 * time.Second}
	var lastErr error
	for attempt := 0; attempt <= len(delays); attempt++ {
		resp, err := client.Models.GenerateContent(ctx, model, contents, config)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		s := err.Error()
		if (!strings.Contains(s, "429") && !strings.Contains(s, "RESOURCE_EXHAUSTED") && !strings.Contains(s, "503") && !strings.Contains(s, "UNAVAILABLE")) || attempt == len(delays) {
			return nil, err
		}
		delay := delays[attempt]
		fmt.Fprintf(os.Stderr, "  rate limited/unavailable, waiting %v before retry %d...\n", delay, attempt+1)
		time.Sleep(delay)
	}
	return nil, lastErr
}

// candidateItem is the raw shape returned by the generator LLM.
type candidateItem struct {
	ID    string   `json:"id"`
	Query string   `json:"query"`
	Tags  []string `json:"tags"`
}

// Category seeds — maps category → target count (sums to ~300).
var categories = map[string]int{
	"date:relative": 60,
	"date:fuzzy":    40,
	"date:absolute": 25,
	"kind:image":    25,
	"kind:video":    15,
	"kind:document": 20,
	"kind:audio":    10,
	"size":          20,
	"extension":     15,
	"path":          10,
	"negation":      15,
	"combo":         35,
	"semantic_only": 10,
}

// generatorPromptTemplate is the prompt sent to Gemini for each category.
const generatorPromptTemplate = `You are generating test data for a file-search query parser evaluation.

Generate {{n}} diverse, realistic user queries for the category "{{category}}".

Category descriptions:
- date:relative → queries with relative time references: "last week", "yesterday", "past 3 days", "this month", etc.
- date:fuzzy → queries with vague time references: "this morning", "a couple months ago", "end of last quarter", "recently", etc.
- date:absolute → queries with specific absolute dates: "march 12 2025", "2026-01-01", "Q1 2025", "January 2025", etc.
- kind:image → queries about image files: photos, pictures, screenshots, etc.
- kind:video → queries about video files: movies, clips, recordings, etc.
- kind:document → queries about document files: PDFs, docs, spreadsheets, presentations, etc.
- kind:audio → queries about audio files: music, podcasts, recordings, etc.
- size → queries with file size constraints: large files, small files, files over 10MB, etc.
- extension → queries with specific file extensions: .pdf, .jpg, .mp4, .docx, etc.
- path → queries with folder/path constraints: files in Downloads, files in projects folder, etc.
- negation → queries that exclude certain types or conditions: not videos, without PDFs, excluding screenshots, etc.
- combo → queries combining two or more constraints: large videos from last month, PDFs from Downloads folder, etc.
- semantic_only → free-text queries with no structured constraints: "meeting notes about Q4 strategy", "design mockup for homepage", etc.

For category "{{category}}", generate {{n}} queries that:
1. Are realistic user queries (as a person would type in a search box)
2. Vary in phrasing, complexity, and specificity
3. Include both simple and complex examples
4. Cover edge cases and common patterns

Return as JSON array of objects with fields: id (string like "cat_001"), query (the search string), tags (array starting with "{{category}}").`

func main() {
	out := flag.String("out", "golden_queries.raw.jsonl", "output path")
	model := flag.String("model", "gemini-2.5-pro", "generator model")
	flag.Parse()

	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		panic("GEMINI_API_KEY must be set")
	}

	client, err := genai.NewClient(context.Background(), &genai.ClientConfig{APIKey: key})
	if err != nil {
		panic(fmt.Sprintf("failed to create genai client: %v", err))
	}

	f, err := os.Create(*out)
	if err != nil {
		panic(fmt.Sprintf("failed to create output file: %v", err))
	}
	defer f.Close()

	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)
	_ = now

	totalWritten := 0
	for cat, n := range categories {
		fmt.Fprintf(os.Stderr, "generating %d cases for category %s...\n", n, cat)
		cases, err := generateForCategory(context.Background(), client, *model, cat, n)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR category %s: %v\n", cat, err)
			continue
		}
		for _, c := range cases {
			b, err := json.Marshal(c)
			if err != nil {
				continue
			}
			fmt.Fprintln(f, string(b))
			totalWritten++
		}
		fmt.Fprintf(os.Stderr, "category %s: wrote %d cases (total so far: %d)\n", cat, len(cases), totalWritten)
		// Respect rate limits: 5 RPM free tier requires at least 12s between calls.
		time.Sleep(13 * time.Second)
	}
	fmt.Fprintf(os.Stderr, "done: %d total candidates written to %s\n", totalWritten, *out)
}

// generateForCategory asks Gemini for N diverse queries for the given category.
func generateForCategory(ctx context.Context, client *genai.Client, model, cat string, n int) ([]candidateItem, error) {
	prompt := strings.NewReplacer(
		"{{category}}", cat,
		"{{n}}", fmt.Sprintf("%d", n),
	).Replace(generatorPromptTemplate)

	// Response schema: array of {id, query, tags}
	schema := &genai.Schema{
		Type: genai.TypeArray,
		Items: &genai.Schema{
			Type:     genai.TypeObject,
			Required: []string{"id", "query", "tags"},
			Properties: map[string]*genai.Schema{
				"id":    {Type: genai.TypeString},
				"query": {Type: genai.TypeString},
				"tags": {
					Type:  genai.TypeArray,
					Items: &genai.Schema{Type: genai.TypeString},
				},
			},
		},
	}

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   schema,
	}

	contents := []*genai.Content{
		{Role: "user", Parts: []*genai.Part{{Text: prompt}}},
	}

	resp, err := retryGenerate(ctx, client, model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("GenerateContent: %w", err)
	}

	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return nil, fmt.Errorf("no candidates returned")
	}

	var text string
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.Text != "" {
			text = part.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("no text in response")
	}

	var items []candidateItem
	if err := json.Unmarshal([]byte(text), &items); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w\nraw: %s", err, text[:min(200, len(text))])
	}

	// Ensure first tag is the category.
	for i := range items {
		if len(items[i].Tags) == 0 {
			items[i].Tags = []string{cat}
		} else if items[i].Tags[0] != cat {
			items[i].Tags = append([]string{cat}, items[i].Tags...)
		}
	}

	return items, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
