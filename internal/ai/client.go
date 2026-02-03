package ai

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/linkedin-agent/internal/config"
	"github.com/linkedin-agent/pkg/logger"
	"github.com/linkedin-agent/pkg/ratelimit"
)

// Client wraps the Anthropic SDK client
type Client struct {
	client      anthropic.Client
	model       string
	maxTokens   int
	temperature float64
	rateLimiter *ratelimit.MultiLimiter
	log         *logger.Logger
}

// NewClient creates a new Anthropic client
func NewClient(cfg config.AnthropicConfig, limiter *ratelimit.MultiLimiter, log *logger.Logger) *Client {
	client := anthropic.NewClient(
		option.WithAPIKey(cfg.APIKey),
	)

	return &Client{
		client:      client,
		model:       cfg.Model,
		maxTokens:   cfg.MaxTokens,
		temperature: cfg.Temperature,
		rateLimiter: limiter,
		log:         log.WithComponent("ai"),
	}
}

// Complete sends a message to Claude and returns the response
func (c *Client) Complete(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	// Wait for rate limiter
	if err := c.rateLimiter.Wait(ctx, ratelimit.LimiterAnthropic); err != nil {
		return "", fmt.Errorf("rate limit error: %w", err)
	}

	c.log.Debug().
		Str("model", c.model).
		Int("max_tokens", c.maxTokens).
		Msg("Sending request to Claude")

	message, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: int64(c.maxTokens),
		System: []anthropic.TextBlockParam{
			{
				Type: "text",
				Text: systemPrompt,
			},
		},
		Messages: []anthropic.MessageParam{
			{
				Role: anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{
					anthropic.NewTextBlock(userMessage),
				},
			},
		},
	})

	if err != nil {
		c.log.Error().Err(err).Msg("Claude API error")
		return "", fmt.Errorf("claude API error: %w", err)
	}

	// Extract text from response
	var response string
	for _, block := range message.Content {
		textBlock := block.AsText()
		if textBlock.Text != "" {
			response += textBlock.Text
		}
	}

	c.log.Debug().
		Int("input_tokens", int(message.Usage.InputTokens)).
		Int("output_tokens", int(message.Usage.OutputTokens)).
		Msg("Received Claude response")

	return response, nil
}

// CompleteWithJSON sends a message and expects a JSON response
func (c *Client) CompleteWithJSON(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	// Add JSON instruction to system prompt
	enhancedSystem := systemPrompt + "\n\nIMPORTANT: Respond ONLY with valid JSON. No markdown, no explanation, just the JSON object."

	return c.Complete(ctx, enhancedSystem, userMessage)
}
