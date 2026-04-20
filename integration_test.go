//go:build integration

package x

import (
	"context"
	"os"
	"testing"
	"time"
)

func cookiesFromEnv() Cookies {
	return Cookies{
		AuthToken: os.Getenv("X_AUTH_TOKEN"),
		CT0:       os.Getenv("X_CT0"),
		Twid:      os.Getenv("X_TWID"),
	}
}

func mustClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(cookiesFromEnv(), WithMinRequestGap(2*time.Second))
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	return c
}

func TestSession(t *testing.T) {
	c := mustClient(t)

	me, err := c.Me(context.Background())
	if err != nil {
		t.Fatalf("Me() error: %v", err)
	}

	t.Logf("Session OK: id=%s screenName=@%s name=%q followers=%d following=%d tweets=%d blue=%v",
		me.ID, me.ScreenName, me.Name, me.FollowersCount, me.FollowingCount, me.TweetCount, me.IsBlueVerified)

	if me.ID == "" {
		t.Error("viewer ID is empty")
	}
	if me.ScreenName == "" {
		t.Error("viewer ScreenName is empty")
	}
}

func TestGetProfile(t *testing.T) {
	c := mustClient(t)
	ctx := context.Background()

	u, err := c.GetProfile(ctx, "elonmusk")
	if err != nil {
		t.Fatalf("GetProfile(elonmusk) error: %v", err)
	}

	t.Logf("Profile: id=%s screenName=@%s name=%q followers=%d following=%d tweets=%d blue=%v",
		u.ID, u.ScreenName, u.Name, u.FollowersCount, u.FollowingCount, u.TweetCount, u.IsBlueVerified)

	if u.ID == "" {
		t.Error("profile ID is empty")
	}
	if u.ScreenName != "elonmusk" {
		t.Errorf("expected screenName=elonmusk, got %s", u.ScreenName)
	}
}

func TestSearchTweets(t *testing.T) {
	c := mustClient(t)
	ctx := context.Background()

	page, err := c.SearchTweets(ctx, "golang", 10, WithSearchType(SearchLatest))
	if err != nil {
		t.Fatalf("SearchTweets() error: %v", err)
	}

	t.Logf("SearchTweets: %d tweets, hasNext=%v", len(page.Tweets), page.HasNext)
	for i, tw := range page.Tweets {
		if i >= 5 {
			t.Logf("  ... and %d more", len(page.Tweets)-5)
			break
		}
		t.Logf("  [%d] id=%s @%s: %s", i, tw.ID, tw.AuthorScreenName, truncate(tw.Text, 80))
	}

	if len(page.Tweets) == 0 {
		t.Skip("no search results returned")
	}
}

func TestHomeTimeline(t *testing.T) {
	c := mustClient(t)
	ctx := context.Background()

	page, err := c.HomeTimeline(ctx, 10)
	if err != nil {
		t.Fatalf("HomeTimeline() error: %v", err)
	}

	t.Logf("HomeTimeline: %d tweets, hasNext=%v", len(page.Tweets), page.HasNext)
	for i, tw := range page.Tweets {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] id=%s @%s likes=%d rt=%d: %s",
			i, tw.ID, tw.AuthorScreenName, tw.LikeCount, tw.RetweetCount, truncate(tw.Text, 80))
	}

	if len(page.Tweets) == 0 {
		t.Skip("no timeline tweets returned")
	}
}

func TestUserTweets(t *testing.T) {
	c := mustClient(t)
	ctx := context.Background()

	me, _ := c.Me(ctx)
	if me == nil {
		t.Skip("no authenticated user")
	}

	page, err := c.UserTweets(ctx, me.ID, 10)
	if err != nil {
		t.Fatalf("UserTweets() error: %v", err)
	}

	t.Logf("UserTweets: %d tweets, hasNext=%v", len(page.Tweets), page.HasNext)
	for i, tw := range page.Tweets {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] id=%s likes=%d rt=%d: %s",
			i, tw.ID, tw.LikeCount, tw.RetweetCount, truncate(tw.Text, 80))
	}
}

func TestGetTweetDetail(t *testing.T) {
	c := mustClient(t)
	ctx := context.Background()

	// First get a tweet to use
	me, _ := c.Me(ctx)
	if me == nil {
		t.Skip("no authenticated user")
	}

	page, err := c.UserTweets(ctx, me.ID, 5)
	if err != nil || len(page.Tweets) == 0 {
		t.Skip("no tweets available for detail test")
	}

	tweetID := page.Tweets[0].ID
	detail, err := c.GetTweetDetail(ctx, tweetID)
	if err != nil {
		t.Fatalf("GetTweetDetail(%s) error: %v", tweetID, err)
	}

	t.Logf("TweetDetail: id=%s text=%s replies=%d",
		detail.Tweet.ID, truncate(detail.Tweet.Text, 80), len(detail.Replies))

	for i, r := range detail.Replies {
		if i >= 3 {
			break
		}
		t.Logf("  reply[%d] id=%s @%s: %s", i, r.ID, r.AuthorScreenName, truncate(r.Text, 60))
	}
}

func TestGetFollowers(t *testing.T) {
	c := mustClient(t)
	ctx := context.Background()

	me, _ := c.Me(ctx)
	if me == nil {
		t.Skip("no authenticated user")
	}

	page, err := c.GetFollowers(ctx, me.ID, 10)
	if err != nil {
		t.Fatalf("GetFollowers() error: %v", err)
	}

	t.Logf("Followers: %d users, hasNext=%v", len(page.Users), page.HasNext)
	for i, u := range page.Users {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] id=%s @%s name=%q", i, u.ID, u.ScreenName, u.Name)
	}
}

func TestGetListTimeline(t *testing.T) {
	c := mustClient(t)
	ctx := context.Background()

	// Use a well-known public list ID for testing.
	// This is Twitter's official @TwitterEng list (may need updating).
	listID := os.Getenv("X_TEST_LIST_ID")
	if listID == "" {
		t.Skip("X_TEST_LIST_ID not set — skipping list timeline test")
	}

	page, err := c.GetListTimeline(ctx, listID, 10)
	if err != nil {
		t.Fatalf("GetListTimeline(%s) error: %v", listID, err)
	}

	t.Logf("ListTimeline: %d tweets, hasNext=%v", len(page.Tweets), page.HasNext)
	for i, tw := range page.Tweets {
		if i >= 5 {
			break
		}
		t.Logf("  [%d] id=%s @%s: %s", i, tw.ID, tw.AuthorScreenName, truncate(tw.Text, 80))
	}
}
