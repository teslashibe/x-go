package mcp

import (
	"context"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

// ScrapeTimelineTrendsInput is the typed input for x_scrape_timeline_trends.
type ScrapeTimelineTrendsInput struct {
	UserID    string   `json:"user_id" jsonschema:"description=numeric X user ID whose tweets to analyse,required"`
	MaxTweets int      `json:"max_tweets,omitempty" jsonschema:"description=cap on tweets fetched for analysis,minimum=1,maximum=5000,default=200"`
	TopN      int      `json:"top_n,omitempty" jsonschema:"description=number of top keywords/hashtags/mentions to return,minimum=1,maximum=200,default=20"`
	StopWords []string `json:"stop_words,omitempty" jsonschema:"description=extra domain-specific stop words to exclude from keyword extraction"`
}

func scrapeTimelineTrends(ctx context.Context, c *x.Client, in ScrapeTimelineTrendsInput) (any, error) {
	var opts []x.TrendOption
	if in.MaxTweets > 0 {
		opts = append(opts, x.WithTrendMaxTweets(in.MaxTweets))
	}
	if in.TopN > 0 {
		opts = append(opts, x.WithTrendTopN(in.TopN))
	}
	if len(in.StopWords) > 0 {
		opts = append(opts, x.WithTrendStopWords(in.StopWords))
	}
	return c.ScrapeTimelineTrends(ctx, in.UserID, opts...)
}

var trendTools = []mcptool.Tool{
	mcptool.Define[*x.Client, ScrapeTimelineTrendsInput](
		"x_scrape_timeline_trends",
		"Paginate a user's tweets and produce a TrendReport (keywords, hashtags, peaks, authors)",
		"ScrapeTimelineTrends",
		scrapeTimelineTrends,
	),
}
