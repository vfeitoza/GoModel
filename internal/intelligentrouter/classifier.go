package intelligentrouter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gomodel/internal/core"
)

// AnalyzerConfig describes one analyzer in the pool.
type AnalyzerConfig struct {
	Model     string
	Provider  string
	MaxTokens int
}

// Classifier calls the analyzer pool to classify a request. Analyzers are tried
// in order; on error or timeout the next one is tried.
type Classifier struct {
	executor  ChatCompletionExecutor
	analyzers []AnalyzerConfig
	maxTokens int
	timeout   time.Duration
	userPath  string // scoped user_path for analyzer usage/audit
}

// NewClassifier constructs a classifier. At least one analyzer is required.
func NewClassifier(executor ChatCompletionExecutor, analyzers []AnalyzerConfig, maxTokens int, timeout time.Duration, userPath string) (*Classifier, error) {
	if executor == nil {
		return nil, fmt.Errorf("intelligent router: executor is required")
	}
	if len(analyzers) == 0 {
		return nil, fmt.Errorf("intelligent router: at least one analyzer is required")
	}
	if maxTokens <= 0 {
		maxTokens = 256
	}
	if timeout <= 0 {
		timeout = 1500 * time.Millisecond
	}
	return &Classifier{
		executor:  executor,
		analyzers: analyzers,
		maxTokens: maxTokens,
		timeout:   timeout,
		userPath:  strings.TrimSpace(userPath),
	}, nil
}

// Analyzers returns the resolved analyzer selectors, in pool order.
func (c *Classifier) Analyzers() []core.ModelSelector {
	out := make([]core.ModelSelector, 0, len(c.analyzers))
	for _, a := range c.analyzers {
		out = append(out, core.ModelSelector{Model: a.Model, Provider: a.Provider})
	}
	return out
}

// Classify runs the analyzer pool against the request and returns the first
// successful classification plus the analyzer that produced it. When every
// analyzer fails, it returns an error so the caller can fall back.
//
// The prompt may optionally include candidate metadata (routing guidance) and a
// short routing history; callers that do not have either can keep using this
// convenience wrapper.
func (c *Classifier) Classify(ctx context.Context, req *core.ChatRequest) (Classification, core.ModelSelector, error) {
	return c.ClassifyWithCandidates(ctx, req, nil, nil)
}

// ClassifyWithCandidates is the full classifier entry point. candidates enrich
// the prompt with model-specific routing guidance when present. history adds the
// most recent routing decisions (conversation-aware routing) in most-recent-last
// order; it is optional and nil-safe.
func (c *Classifier) ClassifyWithCandidates(ctx context.Context, req *core.ChatRequest, candidates []Candidate, history []string) (Classification, core.ModelSelector, error) {
	var zero core.ModelSelector
	if req == nil {
		return Classification{}, zero, fmt.Errorf("intelligent router: request is required")
	}

	pool := c.Analyzers()
	prompt := analyzerUserPrompt(req, candidates, history)
	temperature := 0.0

	var lastErr error
	for _, analyzer := range pool {
		maxTokens := effectiveAnalyzerMaxTokens(c.maxTokensFor(analyzer), c.maxTokens)
		classification, used, err := c.tryAnalyzer(ctx, analyzer, prompt, temperature, maxTokens)
		if err == nil {
			return classification, used, nil
		}
		lastErr = err
		slog.Warn("intelligent router analyzer failed; trying next",
			"analyzer", analyzer.QualifiedModel(),
			"error", err,
		)
	}
	return Classification{}, zero, fmt.Errorf("all intelligent router analyzers failed: %w", lastErr)
}

func (c *Classifier) tryAnalyzer(ctx context.Context, analyzer core.ModelSelector, prompt string, temperature float64, maxTokens int) (Classification, core.ModelSelector, error) {
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	if c.userPath != "" {
		callCtx = core.WithEffectiveUserPath(callCtx, c.userPath)
	}

	resp, err := c.executor.ChatCompletion(callCtx, &core.ChatRequest{
		Model:       analyzer.Model,
		Provider:    analyzer.Provider,
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Messages: []core.Message{
			{Role: "system", Content: analyzerSystemPrompt},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return Classification{}, analyzer, err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return Classification{}, analyzer, fmt.Errorf("analyzer returned no choices")
	}
	content := core.ExtractTextContent(resp.Choices[0].Message.Content)
	if strings.TrimSpace(content) == "" {
		return Classification{}, analyzer, fmt.Errorf("analyzer returned empty content")
	}
	classification, err := parseClassification(content)
	if err == nil {
		return classification, analyzer, nil
	}

	// One repair attempt on the same analyzer before failing over to the next one.
	slog.Warn("intelligent router analyzer returned invalid JSON; attempting repair",
		"analyzer", analyzer.QualifiedModel(),
		"error", err,
	)
	repaired, repairErr := c.repairClassification(callCtx, analyzer, temperature, maxTokens)
	if repairErr == nil {
		slog.Info("intelligent router analyzer repair succeeded",
			"analyzer", analyzer.QualifiedModel(),
		)
		return repaired, analyzer, nil
	}
	return Classification{}, analyzer, fmt.Errorf("parse analyzer response: %w (repair failed: %v)", err, repairErr)
}

func (c *Classifier) repairClassification(ctx context.Context, analyzer core.ModelSelector, temperature float64, maxTokens int) (Classification, error) {
	resp, err := c.executor.ChatCompletion(ctx, &core.ChatRequest{
		Model:       analyzer.Model,
		Provider:    analyzer.Provider,
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
		Messages: []core.Message{
			{Role: "system", Content: analyzerSystemPrompt},
			{Role: "user", Content: "Your previous response was invalid JSON. Return ONLY the compact JSON object described in the system prompt."},
		},
	})
	if err != nil {
		return Classification{}, err
	}
	if resp == nil || len(resp.Choices) == 0 {
		return Classification{}, fmt.Errorf("repair analyzer returned no choices")
	}
	content := core.ExtractTextContent(resp.Choices[0].Message.Content)
	if strings.TrimSpace(content) == "" {
		return Classification{}, fmt.Errorf("repair analyzer returned empty content")
	}
	return parseClassification(content)
}

func (c *Classifier) maxTokensFor(analyzer core.ModelSelector) int {
	for _, a := range c.analyzers {
		if a.Model == analyzer.Model && a.Provider == analyzer.Provider {
			return a.MaxTokens
		}
	}
	return 0
}

func effectiveAnalyzerMaxTokens(perAnalyzer, poolDefault int) int {
	if perAnalyzer > 0 {
		return perAnalyzer
	}
	if poolDefault > 0 {
		return poolDefault
	}
	return 256
}
