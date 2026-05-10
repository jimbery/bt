package ai

import (
	"context"
)

type stubProvider struct {
	text string
}

func (s *stubProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return CompletionResponse{}, err
	}
	in := estimateTokens(req.SystemPrompt + req.UserPrompt)
	out := estimateTokens(s.text)
	return CompletionResponse{
		Text:         s.text,
		InputTokens:  clampTokens(in),
		OutputTokens: clampTokens(out),
	}, nil
}
