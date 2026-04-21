# x-go

Go client for [X (formerly Twitter)](https://x.com) internal APIs. Zero dependencies, cookie-based auth.

```bash
go get github.com/teslashibe/x-go
```

## Quick start

```go
import x "github.com/teslashibe/x-go"

c, _ := x.New(x.Cookies{
    AuthToken: os.Getenv("X_AUTH_TOKEN"),
    CT0:       os.Getenv("X_CT0"),
    Twid:      os.Getenv("X_TWID"),
})
ctx := context.Background()

profile, _ := c.GetProfile(ctx, "elonmusk")
timeline, _ := c.HomeTimeline(ctx, 20)
results, _ := c.SearchTweets(ctx, "golang", 20)
tweet, _ := c.CreateTweet(ctx, "Hello from x-go!")
_ = c.Like(ctx, tweet.ID)
_ = c.Follow(ctx, profile.ID)
_ = c.SendDM(ctx, conversationID, "Hey!")
```

## Authentication

Export session cookies from a logged-in browser session. No API keys or developer app registration required.

```bash
export X_AUTH_TOKEN="9220b5d6a5926..."
export X_CT0="a1e823788453..."
export X_TWID="u%3D123456789"
```

| Cookie | Header | Required | Purpose |
|---|---|---|---|
| `auth_token` | `X_AUTH_TOKEN` | Yes | Primary session credential |
| `ct0` | `X_CT0` | Yes | CSRF token |
| `twid` | `X_TWID` | No | User ID (`u=<restId>`); used to derive authenticated user |

## Features

### Profiles

```go
user, _ := c.GetProfile(ctx, "elonmusk")      // by handle
user, _ = c.GetProfileByID(ctx, "44196397")    // by numeric ID
me, _ := c.Me(ctx)                             // authenticated user
```

### Timelines

```go
page, _ := c.HomeTimeline(ctx, 20)              // algorithmic (For You)
page, _ = c.HomeLatestTimeline(ctx, 20)          // reverse-chronological (Following)

// Cursor pagination
page, _ = c.HomeTimelinePage(ctx, 20, page.NextCursor)
page, _ = c.HomeLatestTimelinePage(ctx, 20, page.NextCursor)
```

### Search

```go
tweets, _ := c.SearchTweets(ctx, "golang", 20,
    x.WithSearchType(x.SearchLatest),
    x.WithSearchSince("2025-01-01"),
    x.WithSearchUntil("2025-06-01"),
)

users, _ := c.SearchUsers(ctx, "golang", 20)

// Cursor pagination
tweets, _ = c.SearchTweetsPage(ctx, "golang", 20, tweets.NextCursor)
```

Search types: `SearchTop`, `SearchLatest`, `SearchPeople`, `SearchMedia`, `SearchLists`

### Tweets

```go
tweet, _ := c.GetTweet(ctx, tweetID)             // single tweet
detail, _ := c.GetTweetDetail(ctx, tweetID)       // tweet + reply thread
page, _ := c.UserTweets(ctx, userID, 20)          // user's tweets
page, _ = c.UserTweetsPage(ctx, userID, 20, cursor)
```

### Social graph

```go
followers, _ := c.GetFollowers(ctx, userID, 20)
following, _ := c.GetFollowing(ctx, userID, 20)

// Cursor pagination
followers, _ = c.GetFollowersPage(ctx, userID, 20, followers.NextCursor)
following, _ = c.GetFollowingPage(ctx, userID, 20, following.NextCursor)
```

### Lists

```go
list, _ := c.GetList(ctx, listID)
tweets, _ := c.GetListTimeline(ctx, listID, 20)
members, _ := c.GetListMembers(ctx, listID, 20)

// Cursor pagination
tweets, _ = c.GetListTimelinePage(ctx, listID, 20, tweets.NextCursor)
```

### Trend analysis

```go
report, _ := c.ScrapeTimelineTrends(ctx, userID,
    x.WithTrendMaxTweets(500),
    x.WithTrendTopN(30),
    x.WithTrendStopWords([]string{"promo", "giveaway"}),
)

fmt.Println(report.TweetsAnalyzed)  // tweets scanned
fmt.Println(report.TopKeywords)     // keyword frequency
fmt.Println(report.TopHashtags)     // hashtag frequency
fmt.Println(report.TopMentions)     // mention frequency
fmt.Println(report.AvgEngagement)   // mean likes+RT+replies+quotes
fmt.Println(report.PeakHours)       // UTC hours ranked by activity
fmt.Println(report.ActiveAuthors)   // most active authors
```

### Tweet composition

```go
tweet, _ := c.CreateTweet(ctx, "Hello world!")                         // 280-char validated
tweet, _ = c.Reply(ctx, tweetID, "Great thread!")
tweet, _ = c.QuoteTweet(ctx, "https://x.com/user/status/123", "This")
_ = c.DeleteTweet(ctx, tweet.ID)

// With media or sensitivity flag
tweet, _ = c.CreateTweet(ctx, "Check this out",
    x.WithMediaIDs("media_id_1", "media_id_2"),
    x.WithPossiblySensitive(),
)
```

### Engagement

```go
_ = c.Like(ctx, tweetID)
_ = c.Unlike(ctx, tweetID)
_ = c.Retweet(ctx, tweetID)
_ = c.Unretweet(ctx, tweetID)
_ = c.Bookmark(ctx, tweetID)
_ = c.Unbookmark(ctx, tweetID)
```

### Social actions

```go
_ = c.Follow(ctx, userID)
_ = c.Unfollow(ctx, userID)
_ = c.Mute(ctx, userID)
_ = c.Unmute(ctx, userID)
_ = c.Block(ctx, userID)
_ = c.Unblock(ctx, userID)
```

### Direct messages

```go
msg, _ := c.SendDM(ctx, conversationID, "Hey!")
convos, _ := c.GetConversations(ctx)
msgs, _ := c.GetConversation(ctx, conversationID)
```

## Configuration

```go
c, _ := x.New(cookies,
    x.WithMinRequestGap(2*time.Second),   // leaky-bucket gap (default 1s)
    x.WithRetry(5, 1*time.Second),        // max attempts + backoff base (default 3, 500ms)
    x.WithProxy("http://127.0.0.1:8080"), // route through proxy
    x.WithUserAgent("my-bot/1.0"),        // custom User-Agent
    x.WithQueryIDs(map[string]string{     // override stale queryIds
        "HomeTimeline": "newQueryId123",
    }),
)
```

## QueryID rotation

X rotates GraphQL queryIds with each deploy (roughly every 2–4 weeks). The client ships with baked-in defaults, but they go stale. Two options:

```go
// Option 1: auto-refresh from X's main.js bundle
err := c.RefreshQueryIDs(ctx)

// Option 2: pass known-good IDs at construction
c, _ := x.New(cookies, x.WithQueryIDs(map[string]string{
    "HomeTimeline":   "abc123",
    "SearchTimeline": "def456",
}))
```

## Transport

- **stdlib only** — zero `require` entries in `go.mod`
- **Leaky-bucket rate limiting** — configurable minimum gap between requests (default 1s)
- **Exponential backoff** — retries with `500ms × 2^n` jitter on transient failures
- **`X-Client-Transaction-Id` header** — generated via X's animation-key algorithm to bypass CDN bot detection
- **10 MB body cap** — all response bodies are limited to prevent memory exhaustion
- **Thread-safe** — `Client` is safe for concurrent use from multiple goroutines
- **`X-Rate-Limit-Reset` parsing** — respects Unix-timestamp, seconds, and HTTP-date formats

## Error handling

All errors are sentinel-wrapped for programmatic handling:

```go
if errors.Is(err, x.ErrUnauthorized)      { /* session expired */ }
if errors.Is(err, x.ErrForbidden)         { /* protected account */ }
if errors.Is(err, x.ErrNotFound)          { /* user/tweet doesn't exist */ }
if errors.Is(err, x.ErrRateLimited)       { /* slow down */ }
if errors.Is(err, x.ErrSuspended)         { /* account suspended */ }
if errors.Is(err, x.ErrQueryIDStale)      { /* call RefreshQueryIDs */ }
if errors.Is(err, x.ErrTweetTooLong)      { /* >280 characters */ }
if errors.Is(err, x.ErrAlreadyRetweeted)  { /* duplicate retweet */ }
if errors.Is(err, x.ErrDMClosed)          { /* recipient has DMs closed */ }
if errors.Is(err, x.ErrPartialResult)     { /* context cancelled mid-scrape */ }
```

## Testing

```bash
export X_AUTH_TOKEN="..."
export X_CT0="..."
export X_TWID="..."

go test -tags integration -v -count=1 ./...
```
