package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/teslashibe/mcptool"
	x "github.com/teslashibe/x-go"
)

func TestProjectTweetDefaultAndFullReturnOriginalTweet(t *testing.T) {
	tw := testTweet()

	for _, view := range []string{"", "full"} {
		got, err := projectTweet(tw, view)
		if err != nil {
			t.Fatalf("projectTweet(view=%q) error: %v", view, err)
		}
		if got != tw {
			t.Fatalf("projectTweet(view=%q) = %#v, want original tweet pointer", view, got)
		}
	}
}

func TestProjectTweetMetricsView(t *testing.T) {
	tw := testTweet()

	got, err := projectTweet(tw, "metrics")
	if err != nil {
		t.Fatalf("projectTweet(metrics) error: %v", err)
	}

	metrics, ok := got.(tweetMetricsView)
	if !ok {
		t.Fatalf("projectTweet(metrics) returned %T, want tweetMetricsView", got)
	}
	if metrics.ID != tw.ID {
		t.Errorf("ID = %q, want %q", metrics.ID, tw.ID)
	}
	if metrics.AuthorScreenName != tw.AuthorScreenName {
		t.Errorf("AuthorScreenName = %q, want %q", metrics.AuthorScreenName, tw.AuthorScreenName)
	}
	if !metrics.CreatedAt.Equal(tw.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", metrics.CreatedAt, tw.CreatedAt)
	}
	assertEngagementCounts(t, metrics.tweetEngagementCounts, tw)
	assertJSONKeys(t, got, []string{
		"id",
		"authorScreenName",
		"createdAt",
		"likeCount",
		"retweetCount",
		"replyCount",
		"quoteCount",
		"bookmarkCount",
		"viewCount",
	})
}

func TestProjectTweetCompactView(t *testing.T) {
	tw := testTweet()

	got, err := projectTweet(tw, "compact")
	if err != nil {
		t.Fatalf("projectTweet(compact) error: %v", err)
	}

	compact, ok := got.(tweetCompactView)
	if !ok {
		t.Fatalf("projectTweet(compact) returned %T, want tweetCompactView", got)
	}
	if compact.ID != tw.ID {
		t.Errorf("ID = %q, want %q", compact.ID, tw.ID)
	}
	if compact.ConversationID != tw.ConversationID {
		t.Errorf("ConversationID = %q, want %q", compact.ConversationID, tw.ConversationID)
	}
	if compact.AuthorID != tw.AuthorID {
		t.Errorf("AuthorID = %q, want %q", compact.AuthorID, tw.AuthorID)
	}
	if compact.AuthorScreenName != tw.AuthorScreenName {
		t.Errorf("AuthorScreenName = %q, want %q", compact.AuthorScreenName, tw.AuthorScreenName)
	}
	if compact.AuthorName != tw.AuthorName {
		t.Errorf("AuthorName = %q, want %q", compact.AuthorName, tw.AuthorName)
	}
	if compact.Text != tw.Text {
		t.Errorf("Text = %q, want %q", compact.Text, tw.Text)
	}
	if !compact.CreatedAt.Equal(tw.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", compact.CreatedAt, tw.CreatedAt)
	}
	if compact.IsRetweet != tw.IsRetweet {
		t.Errorf("IsRetweet = %v, want %v", compact.IsRetweet, tw.IsRetweet)
	}
	if compact.IsQuote != tw.IsQuote {
		t.Errorf("IsQuote = %v, want %v", compact.IsQuote, tw.IsQuote)
	}
	if compact.IsReply != tw.IsReply {
		t.Errorf("IsReply = %v, want %v", compact.IsReply, tw.IsReply)
	}
	if compact.InReplyToID != tw.InReplyToID {
		t.Errorf("InReplyToID = %q, want %q", compact.InReplyToID, tw.InReplyToID)
	}
	if compact.QuotedTweetID != tw.QuotedTweetID {
		t.Errorf("QuotedTweetID = %q, want %q", compact.QuotedTweetID, tw.QuotedTweetID)
	}
	assertEngagementCounts(t, compact.tweetEngagementCounts, tw)
	assertJSONKeys(t, got, []string{
		"id",
		"conversationId",
		"authorId",
		"authorScreenName",
		"authorName",
		"text",
		"createdAt",
		"isRetweet",
		"isQuote",
		"isReply",
		"inReplyToId",
		"quotedTweetId",
		"likeCount",
		"retweetCount",
		"replyCount",
		"quoteCount",
		"bookmarkCount",
		"viewCount",
	})
}

func TestProjectTweetInvalidView(t *testing.T) {
	_, err := projectTweet(testTweet(), "summary")
	if !errors.Is(err, x.ErrInvalidParams) {
		t.Fatalf("projectTweet(invalid) error = %v, want ErrInvalidParams", err)
	}
}

func TestProjectTweetPageDefaultAndFullReturnTweetPage(t *testing.T) {
	tweets := testTweets(2)

	for _, view := range []string{"", "full"} {
		got, err := projectTweetPage(tweets, "next", 20, view)
		if err != nil {
			t.Fatalf("projectTweetPage(view=%q) error: %v", view, err)
		}

		page, ok := got.(mcptool.Page[x.Tweet])
		if !ok {
			t.Fatalf("projectTweetPage(view=%q) returned %T, want mcptool.Page[x.Tweet]", view, got)
		}
		if !reflect.DeepEqual(page.Items, tweets) {
			t.Errorf("Items = %#v, want %#v", page.Items, tweets)
		}
		if page.NextCursor != "next" {
			t.Errorf("NextCursor = %q, want next", page.NextCursor)
		}
		if page.Truncated {
			t.Error("Truncated = true, want false")
		}
	}
}

func TestProjectTweetPageCompactView(t *testing.T) {
	tweets := testTweets(2)

	got, err := projectTweetPage(tweets, "next", 20, "compact")
	if err != nil {
		t.Fatalf("projectTweetPage(compact) error: %v", err)
	}

	page, ok := got.(mcptool.Page[tweetCompactView])
	if !ok {
		t.Fatalf("projectTweetPage(compact) returned %T, want mcptool.Page[tweetCompactView]", got)
	}
	if len(page.Items) != len(tweets) {
		t.Fatalf("len(Items) = %d, want %d", len(page.Items), len(tweets))
	}
	for i := range tweets {
		if page.Items[i].ID != tweets[i].ID {
			t.Errorf("Items[%d].ID = %q, want %q", i, page.Items[i].ID, tweets[i].ID)
		}
		if page.Items[i].Text != tweets[i].Text {
			t.Errorf("Items[%d].Text = %q, want %q", i, page.Items[i].Text, tweets[i].Text)
		}
		assertEngagementCounts(t, page.Items[i].tweetEngagementCounts, &tweets[i])
	}
	if page.NextCursor != "next" {
		t.Errorf("NextCursor = %q, want next", page.NextCursor)
	}
}

func TestProjectTweetPageMetricsView(t *testing.T) {
	tweets := testTweets(2)

	got, err := projectTweetPage(tweets, "next", 20, "metrics")
	if err != nil {
		t.Fatalf("projectTweetPage(metrics) error: %v", err)
	}

	page, ok := got.(mcptool.Page[tweetMetricsView])
	if !ok {
		t.Fatalf("projectTweetPage(metrics) returned %T, want mcptool.Page[tweetMetricsView]", got)
	}
	if len(page.Items) != len(tweets) {
		t.Fatalf("len(Items) = %d, want %d", len(page.Items), len(tweets))
	}
	for i := range tweets {
		if page.Items[i].ID != tweets[i].ID {
			t.Errorf("Items[%d].ID = %q, want %q", i, page.Items[i].ID, tweets[i].ID)
		}
		if page.Items[i].AuthorScreenName != tweets[i].AuthorScreenName {
			t.Errorf("Items[%d].AuthorScreenName = %q, want %q", i, page.Items[i].AuthorScreenName, tweets[i].AuthorScreenName)
		}
		assertEngagementCounts(t, page.Items[i].tweetEngagementCounts, &tweets[i])
	}
	if page.NextCursor != "next" {
		t.Errorf("NextCursor = %q, want next", page.NextCursor)
	}
}

func TestProjectTweetPagePreservesPaginationLimit(t *testing.T) {
	tweets := testTweets(3)

	got, err := projectTweetPage(tweets, "next", 2, "compact")
	if err != nil {
		t.Fatalf("projectTweetPage(compact) error: %v", err)
	}

	page, ok := got.(mcptool.Page[tweetCompactView])
	if !ok {
		t.Fatalf("projectTweetPage(compact) returned %T, want mcptool.Page[tweetCompactView]", got)
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(page.Items))
	}
	if page.NextCursor != "next" {
		t.Errorf("NextCursor = %q, want next", page.NextCursor)
	}
	if !page.Truncated {
		t.Error("Truncated = false, want true")
	}
}

func TestProjectTweetPageInvalidView(t *testing.T) {
	_, err := projectTweetPage(testTweets(1), "next", 20, "summary")
	if !errors.Is(err, x.ErrInvalidParams) {
		t.Fatalf("projectTweetPage(invalid) error = %v, want ErrInvalidParams", err)
	}
}

func testTweet() *x.Tweet {
	return &x.Tweet{
		ID:               "123",
		ConversationID:   "456",
		AuthorID:         "789",
		AuthorScreenName: "teslashibe",
		AuthorName:       "Tesla Shibe",
		Text:             "compact metrics test",
		CreatedAt:        time.Date(2026, 6, 23, 10, 30, 0, 0, time.UTC),
		LikeCount:        11,
		RetweetCount:     12,
		ReplyCount:       13,
		QuoteCount:       14,
		BookmarkCount:    15,
		ViewCount:        16,
		Language:         "en",
		IsRetweet:        true,
		IsQuote:          true,
		IsReply:          true,
		InReplyToID:      "321",
		QuotedTweetID:    "654",
		MediaURLs:        []string{"https://example.com/media.jpg"},
		Hashtags:         []string{"xgo"},
		MentionedUsers:   []string{"agent"},
		URLs:             []string{"https://example.com"},
	}
}

func testTweets(n int) []x.Tweet {
	tweets := make([]x.Tweet, n)
	for i := range tweets {
		tw := *testTweet()
		tw.ID = fmt.Sprintf("tweet-%d", i)
		tw.AuthorScreenName = fmt.Sprintf("author%d", i)
		tw.Text = fmt.Sprintf("tweet text %d", i)
		tw.LikeCount += i
		tweets[i] = tw
	}
	return tweets
}

func assertEngagementCounts(t *testing.T, got tweetEngagementCounts, tw *x.Tweet) {
	t.Helper()
	if got.LikeCount != tw.LikeCount {
		t.Errorf("LikeCount = %d, want %d", got.LikeCount, tw.LikeCount)
	}
	if got.RetweetCount != tw.RetweetCount {
		t.Errorf("RetweetCount = %d, want %d", got.RetweetCount, tw.RetweetCount)
	}
	if got.ReplyCount != tw.ReplyCount {
		t.Errorf("ReplyCount = %d, want %d", got.ReplyCount, tw.ReplyCount)
	}
	if got.QuoteCount != tw.QuoteCount {
		t.Errorf("QuoteCount = %d, want %d", got.QuoteCount, tw.QuoteCount)
	}
	if got.BookmarkCount != tw.BookmarkCount {
		t.Errorf("BookmarkCount = %d, want %d", got.BookmarkCount, tw.BookmarkCount)
	}
	if got.ViewCount != tw.ViewCount {
		t.Errorf("ViewCount = %d, want %d", got.ViewCount, tw.ViewCount)
	}
}

func assertJSONKeys(t *testing.T, v any, want []string) {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal(%T) error: %v", v, err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal(%T) error: %v", v, err)
	}

	wantSet := make(map[string]bool, len(want))
	for _, key := range want {
		wantSet[key] = true
	}
	gotSet := make(map[string]bool, len(got))
	for key := range got {
		gotSet[key] = true
	}
	if !reflect.DeepEqual(gotSet, wantSet) {
		t.Fatalf("json keys = %v, want %v", gotSet, wantSet)
	}
}
