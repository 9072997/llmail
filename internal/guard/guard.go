package guard

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/jpennington/llmail/internal/config"
	"github.com/knights-analytics/hugot"
	"github.com/knights-analytics/hugot/pipelines"
)

// Approximate characters per token for DeBERTa on English text.
// Conservative estimate: real ratio is ~4, using 3 to avoid under-scanning.
const charsPerToken = 3

const (
	maxChars   = 512 * charsPerToken  // ~1536 chars ≈ 512 tokens
	strideChar = 384 * charsPerToken  // ~1152 chars ≈ 384 tokens (128 token overlap)
)

type ScanResult struct {
	IsMalicious bool
	Confidence  float64
}

type Guard struct {
	pipeline  *pipelines.TextClassificationPipeline
	session   *hugot.Session
	threshold float64
	mu        sync.Mutex
}

func New(cfg config.GuardConfig) (*Guard, error) {
	modelPath := cfg.ModelPath
	if modelPath == "" {
		modelPath = DefaultModelPath()
	}

	session, err := hugot.NewGoSession()
	if err != nil {
		return nil, fmt.Errorf("creating inference session: %w", err)
	}

	pipeline, err := hugot.NewPipeline(session, hugot.TextClassificationConfig{
		ModelPath: modelPath,
		Name:      "prompt-guard",
	})
	if err != nil {
		session.Destroy()
		return nil, fmt.Errorf("creating classification pipeline: %w", err)
	}

	return &Guard{
		pipeline:  pipeline,
		session:   session,
		threshold: cfg.Threshold,
	}, nil
}

// Check scans text for prompt injection. Returns an error if injection is
// detected, nil if the text is safe. The ScanResult is always returned for
// logging/debug purposes. Safe to call on a nil Guard.
func (g *Guard) Check(text string) (ScanResult, error) {
	if g == nil {
		return ScanResult{}, nil
	}
	result, err := g.Scan(text)
	if err != nil {
		return ScanResult{}, fmt.Errorf("prompt injection scan failed: %w", err)
	}
	if result.IsMalicious {
		return result, fmt.Errorf("content blocked: prompt injection detected (confidence: %.0f%%)", result.Confidence*100)
	}
	return result, nil
}

// Scan runs the prompt injection model on the text and returns detailed results.
// For text longer than ~512 tokens, a sliding window with overlap is used.
func (g *Guard) Scan(text string) (ScanResult, error) {
	if g == nil {
		return ScanResult{}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	chunks := splitIntoWindows(text)

	var best ScanResult
	for _, chunk := range chunks {
		result, err := g.classifyChunk(chunk)
		if err != nil {
			return ScanResult{}, fmt.Errorf("guard: chunk classification failed: %w", err)
		}

		for _, outputs := range result.ClassificationOutputs {
			for _, o := range outputs {
				if o.Label == "MALICIOUS" {
					conf := float64(o.Score)
					if conf > best.Confidence {
						best.Confidence = conf
						best.IsMalicious = conf >= g.threshold
					}
				}
			}
		}
	}

	return best, nil
}

// classifyChunk runs inference on a single chunk. Panics in the upstream
// tokenizer are recovered and returned as errors (fail-closed).
func (g *Guard) classifyChunk(chunk string) (result *pipelines.TextClassificationOutput, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("tokenizer panic: %v", r)
			dumpChunk(chunk, err)
		}
	}()
	return g.pipeline.RunPipeline([]string{chunk})
}

// dumpChunk writes a failing chunk to a temp file for debugging.
func dumpChunk(chunk string, err error) {
	var buf [8]byte
	rand.Read(buf[:])
	name := "guard-fail-" + hex.EncodeToString(buf[:]) + ".txt"
	path := filepath.Join(os.TempDir(), name)
	if writeErr := os.WriteFile(path, []byte(chunk), 0600); writeErr != nil {
		slog.Warn("guard: failed to dump chunk", "error", writeErr)
		return
	}
	slog.Warn("guard: wrote failing chunk for debugging", "path", path, "error", err)
}

// splitIntoWindows splits text into overlapping character-based windows.
// Each window is approximately 512 tokens with 128-token overlap.
// The model's own tokenizer handles the exact tokenization internally.
func splitIntoWindows(text string) []string {
	if len(text) <= maxChars {
		return []string{text}
	}

	var chunks []string
	runes := []rune(text)
	for start := 0; start < len(runes); start += strideChar {
		end := start + maxChars
		if end > len(runes) {
			end = len(runes)
		}

		chunks = append(chunks, string(runes[start:end]))

		if end == len(runes) {
			break
		}
	}

	return chunks
}

func (g *Guard) Close() {
	if g == nil {
		return
	}
	if g.session != nil {
		g.session.Destroy()
	}
}
