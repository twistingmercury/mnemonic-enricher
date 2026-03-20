package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/twistingmercury/mnemonic/internal/config"
)

// ErrExtractionFailed is returned when concept extraction fails after all retries.
var ErrExtractionFailed = errors.New("concept extraction failed")

// Concept represents an entity extracted from pattern content.
type Concept struct {
	Name string `json:"name"` // Normalized to lowercase.
	Type string `json:"type"` // "technology", "practice", or "domain".
}

// ExtractionService extracts structured concepts from text using an LLM.
// MVP implementation calls OpenAI gpt-4o-mini chat completions.
type ExtractionService interface {
	// Extract identifies concepts, technologies, and practices in the text.
	// Returns a slice of Concept with normalized lowercase names.
	// Returns ErrExtractionFailed if the API call fails after retries.
	Extract(ctx context.Context, text string) ([]Concept, error)
}

// chatCompletionsEndpoint is the default OpenAI chat completions API URL.
const chatCompletionsEndpoint = "https://api.openai.com/v1/chat/completions"

// systemPrompt instructs the LLM to extract entities from pattern content.
const systemPrompt = `Extract key concepts from this pattern document.

Return JSON with:
- concepts: General programming concepts
- technologies: Languages, frameworks, tools
- practices: Best practices, patterns, methodologies

Return ONLY valid JSON with no additional text. Example format:
{"concepts": ["error handling"], "technologies": ["Go"], "practices": ["defensive programming"]}`

type openaiExtraction struct {
	client     *http.Client
	baseURL    string
	apiKey     string
	model      string
	retries    int
	retryDelay time.Duration
}

// NewExtractionService creates an ExtractionService backed by the OpenAI chat completions API.
func NewExtractionService(cfg config.OpenAIConfig) ExtractionService {
	return &openaiExtraction{
		client:     &http.Client{Timeout: 60 * time.Second},
		baseURL:    chatCompletionsEndpoint,
		apiKey:     cfg.APIKey,
		model:      cfg.ExtractionModel,
		retries:    cfg.RetryAttempts,
		retryDelay: cfg.RetryDelay,
	}
}

// newExtractionServiceWithURL creates an ExtractionService pointing at a custom URL.
// This is used for testing with httptest servers.
func newExtractionServiceWithURL(cfg config.OpenAIConfig, baseURL string) ExtractionService {
	svc := NewExtractionService(cfg).(*openaiExtraction)
	svc.baseURL = baseURL
	return svc
}

// chatRequest is the OpenAI chat completions API request body.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

// chatMessage represents a single message in a chat completion request.
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatResponse is the OpenAI chat completions API response body.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// extractionResult holds the raw LLM extraction output before mapping to Concepts.
type extractionResult struct {
	Concepts     []string `json:"concepts"`
	Technologies []string `json:"technologies"`
	Practices    []string `json:"practices"`
}

// Extract identifies concepts, technologies, and practices in the given text.
func (e *openaiExtraction) Extract(ctx context.Context, text string) ([]Concept, error) {
	var lastErr error

	for attempt := range e.retries + 1 {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrExtractionFailed, err)
		}

		concepts, err := e.doExtract(ctx, text)
		if err == nil {
			return concepts, nil
		}
		lastErr = err

		// Sleep before retry, unless this was the last attempt.
		if attempt < e.retries {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: %v", ErrExtractionFailed, ctx.Err())
			case <-time.After(e.retryDelay):
			}
		}
	}

	return nil, fmt.Errorf("%w: %v", ErrExtractionFailed, lastErr)
}

// doExtract performs a single extraction API call.
func (e *openaiExtraction) doExtract(ctx context.Context, text string) ([]Concept, error) {
	reqBody := chatRequest{
		Model: e.model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "Pattern content:\n" + text},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(req) // #nosec G704 -- baseURL is from config, not user input
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("unmarshal chat response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("empty chat response: no choices returned")
	}

	content := chatResp.Choices[0].Message.Content
	return parseConcepts(content)
}

// parseConcepts parses the LLM JSON output into a normalized slice of Concept.
func parseConcepts(content string) ([]Concept, error) {
	var result extractionResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("unmarshal extraction result: %w", err)
	}

	var concepts []Concept

	for _, name := range result.Concepts {
		concepts = append(concepts, Concept{
			Name: strings.ToLower(strings.TrimSpace(name)),
			Type: "domain",
		})
	}
	for _, name := range result.Technologies {
		concepts = append(concepts, Concept{
			Name: strings.ToLower(strings.TrimSpace(name)),
			Type: "technology",
		})
	}
	for _, name := range result.Practices {
		concepts = append(concepts, Concept{
			Name: strings.ToLower(strings.TrimSpace(name)),
			Type: "practice",
		})
	}

	return concepts, nil
}
