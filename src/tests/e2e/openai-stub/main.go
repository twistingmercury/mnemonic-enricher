package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
)

var failNext int32

func requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") == "" {
		http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
		return false
	}
	return true
}

func handleEmbeddings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if !requireAuth(w, r) {
		return
	}
	if atomic.CompareAndSwapInt32(&failNext, 1, 0) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"forced failure"}`))
		return
	}

	embedding := make([]float64, 2000)
	for i := range embedding {
		embedding[i] = 0.1
	}

	type embeddingItem struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float64 `json:"embedding"`
	}
	type usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	}
	type response struct {
		Object string          `json:"object"`
		Data   []embeddingItem `json:"data"`
		Model  string          `json:"model"`
		Usage  usage           `json:"usage"`
	}

	resp := response{
		Object: "list",
		Data: []embeddingItem{
			{Object: "embedding", Index: 0, Embedding: embedding},
		},
		Model: "text-embedding-3-small",
		Usage: usage{PromptTokens: 8, TotalTokens: 8},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("embeddings encode error: %v", err)
	}
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if !requireAuth(w, r) {
		return
	}

	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type choice struct {
		Message message `json:"message"`
	}
	type response struct {
		Choices []choice `json:"choices"`
	}

	resp := response{
		Choices: []choice{
			{
				Message: message{
					Role:    "assistant",
					Content: `{"concepts":["pattern","design"],"technologies":["go"],"practices":["testing"]}`,
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("chat completions encode error: %v", err)
	}
}

func handleFailNext(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	atomic.StoreInt32(&failNext, 1)
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/embeddings", handleEmbeddings)
	mux.HandleFunc("/v1/chat/completions", handleChatCompletions)
	mux.HandleFunc("/control/fail-next", handleFailNext)

	log.Println("OpenAI stub listening on :8090")
	if err := http.ListenAndServe(":8090", mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
