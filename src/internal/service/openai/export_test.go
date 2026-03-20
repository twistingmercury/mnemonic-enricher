package openai

import "github.com/twistingmercury/mnemonic/internal/config"

// NewEmbeddingServiceForTest creates an EmbeddingService pointing at a custom URL.
// Exported for use in black-box tests.
var NewEmbeddingServiceForTest = newEmbeddingServiceWithURL

// NewExtractionServiceForTest creates an ExtractionService pointing at a custom URL.
// Exported for use in black-box tests.
var NewExtractionServiceForTest = func(cfg config.OpenAIConfig, baseURL string) ExtractionService {
	return newExtractionServiceWithURL(cfg, baseURL)
}
