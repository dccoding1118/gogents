package source

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/logger"
)

type githubIssueContentFetcher struct {
	cfg foundation.Config
	lg  logger.Logger
	ghRunner
}

func newGitHubIssueContentFetcher(cfg foundation.Config, lg logger.Logger) TaskContentFetcher {
	return &githubIssueContentFetcher{cfg: cfg, lg: lg, ghRunner: ghRunner{lg: lg}}
}

type ghIssueDetail struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
	Comments []struct {
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Body      string `json:"body"`
		CreatedAt string `json:"createdAt"`
	} `json:"comments"`
}

func (f *githubIssueContentFetcher) Fetch(ctx context.Context, taskID string) (TaskContent, error) {
	number, ok := parseGitHubIssueNumber(taskID)
	if !ok {
		return TaskContent{}, ErrSourceProgrammer
	}

	args := []string{
		"issue", "view", strconv.Itoa(number),
		"--repo", f.cfg.Source.Instance,
		"--json", "url,title,body,author,comments",
	}
	stdout, err := f.runWithRetry(ctx, "gh", args)
	if err != nil {
		return TaskContent{}, err
	}

	var raw ghIssueDetail
	if uErr := json.Unmarshal(stdout, &raw); uErr != nil {
		return TaskContent{}, fmt.Errorf("%w: failed to parse gh output: %v", ErrSourceTransient, uErr)
	}

	content := TaskContent{
		URL:    raw.URL,
		Title:  raw.Title,
		Body:   raw.Body,
		Author: raw.Author.Login,
	}
	for _, c := range raw.Comments {
		if isGogentsFeedbackComment(c.Body) {
			continue
		}
		t, _ := time.Parse(time.RFC3339, c.CreatedAt)
		content.Comments = append(content.Comments, Comment{
			Author:    c.Author.Login,
			CreatedAt: t,
			Body:      c.Body,
		})
	}
	return content, nil
}

// isGogentsFeedbackComment reports whether a comment body was authored by
// FeedbackReporter and should be excluded from the agent's input.md.
//
// Detection order:
//  1. The hidden HTML marker `<!-- gogents-feedback -->` that current
//     FeedbackReporter prepends to every comment body.
//  2. Legacy fallback: bodies that begin with the visible "🤖 Task Syncer"
//     prefix (covers comments written before the marker was introduced).
func isGogentsFeedbackComment(body string) bool {
	trimmed := strings.TrimLeft(body, " \t\r\n")
	if strings.HasPrefix(trimmed, FeedbackMarker) {
		return true
	}
	if strings.HasPrefix(trimmed, "🤖 Task Syncer") {
		return true
	}
	return false
}

// parseGitHubIssueNumber strips the "github-" prefix and parses the trailing number.
func parseGitHubIssueNumber(taskID string) (int, bool) {
	const prefix = "github-"
	if !strings.HasPrefix(taskID, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(taskID[len(prefix):])
	if err != nil {
		return 0, false
	}
	return n, true
}
