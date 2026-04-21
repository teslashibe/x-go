package x

import "time"

// Cookies holds the X session cookies obtained from a browser export.
// AuthToken and CT0 are required; Twid is used to derive the authenticated
// user's REST ID.
type Cookies struct {
	AuthToken string `json:"auth_token"` // auth_token: primary session credential
	CT0       string `json:"ct0"`        // ct0: CSRF token
	Twid      string `json:"twid"`       // twid: "u=<restId>" encoded user identifier
	KDT       string `json:"kdt"`        // kdt: optional device token
}

// User represents an X user profile.
type User struct {
	ID              string    `json:"id"`
	ScreenName      string    `json:"screenName"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	Location        string    `json:"location,omitempty"`
	URL             string    `json:"url,omitempty"`
	ProfileImageURL string    `json:"profileImageUrl,omitempty"`
	BannerURL       string    `json:"bannerUrl,omitempty"`
	FollowersCount  int       `json:"followersCount"`
	FollowingCount  int       `json:"followingCount"`
	TweetCount      int       `json:"tweetCount"`
	ListedCount     int       `json:"listedCount"`
	Verified        bool      `json:"verified"`
	IsBlueVerified  bool      `json:"isBlueVerified"`
	CreatedAt       time.Time `json:"createdAt"`
	PinnedTweetIDs  []string  `json:"pinnedTweetIds,omitempty"`
}

// Tweet represents a single X post.
type Tweet struct {
	ID               string    `json:"id"`
	ConversationID   string    `json:"conversationId,omitempty"`
	AuthorID         string    `json:"authorId"`
	AuthorScreenName string    `json:"authorScreenName"`
	AuthorName       string    `json:"authorName"`
	Text             string    `json:"text"`
	CreatedAt        time.Time `json:"createdAt"`
	LikeCount        int       `json:"likeCount"`
	RetweetCount     int       `json:"retweetCount"`
	ReplyCount       int       `json:"replyCount"`
	QuoteCount       int       `json:"quoteCount"`
	BookmarkCount    int       `json:"bookmarkCount"`
	ViewCount        int       `json:"viewCount"`
	Language         string    `json:"language,omitempty"`
	IsRetweet        bool      `json:"isRetweet"`
	IsQuote          bool      `json:"isQuote"`
	IsReply          bool      `json:"isReply"`
	InReplyToID      string    `json:"inReplyToId,omitempty"`
	QuotedTweetID    string    `json:"quotedTweetId,omitempty"`
	MediaURLs        []string  `json:"mediaUrls,omitempty"`
	Hashtags         []string  `json:"hashtags,omitempty"`
	MentionedUsers   []string  `json:"mentionedUsers,omitempty"`
	URLs             []string  `json:"urls,omitempty"`
}

// TweetPage is one page of tweets with a cursor for the next page.
type TweetPage struct {
	Tweets     []Tweet `json:"tweets"`
	NextCursor string  `json:"nextCursor,omitempty"`
	HasNext    bool    `json:"hasNext"`
}

// UserPage is one page of users with a cursor for the next page.
type UserPage struct {
	Users      []User `json:"users"`
	NextCursor string `json:"nextCursor,omitempty"`
	HasNext    bool   `json:"hasNext"`
}

// TweetDetail holds a tweet together with its conversation replies.
type TweetDetail struct {
	Tweet   Tweet   `json:"tweet"`
	Replies []Tweet `json:"replies,omitempty"`
}

// List represents an X list.
type List struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug,omitempty"`
	Description string `json:"description,omitempty"`
	MemberCount int    `json:"memberCount"`
	OwnerID     string `json:"ownerId,omitempty"`
	OwnerName   string `json:"ownerName,omitempty"`
	IsPrivate   bool   `json:"isPrivate"`
}

// SearchType controls which tab of X search to query.
type SearchType string

const (
	SearchTop    SearchType = "Top"
	SearchLatest SearchType = "Latest"
	SearchPeople SearchType = "People"
	SearchMedia  SearchType = "Media"
	SearchLists  SearchType = "Lists"
)

// TrendReport is the output of ScrapeTimelineTrends.
type TrendReport struct {
	TweetsAnalyzed int              `json:"tweetsAnalyzed"`
	TopKeywords    []KeywordFreq    `json:"topKeywords"`
	TopHashtags    []KeywordFreq    `json:"topHashtags"`
	TopMentions    []KeywordFreq    `json:"topMentions"`
	AvgEngagement  float64          `json:"avgEngagement"`
	PeakHours      []int            `json:"peakHours"`
	ActiveAuthors  []AuthorActivity `json:"activeAuthors"`
}

// KeywordFreq pairs a term with its occurrence count.
type KeywordFreq struct {
	Term  string `json:"term"`
	Count int    `json:"count"`
}

// AuthorActivity summarises one author's posting activity.
type AuthorActivity struct {
	AuthorID   string `json:"authorId"`
	ScreenName string `json:"screenName"`
	TweetCount int    `json:"tweetCount"`
}

// RateLimitState tracks X's rate limit headers from the most recent response.
type RateLimitState struct {
	Limit     int       `json:"limit"`     // x-rate-limit-limit: max requests per window
	Remaining int       `json:"remaining"` // x-rate-limit-remaining: requests left in window
	Reset     time.Time `json:"reset"`     // x-rate-limit-reset: when the window resets (UTC)
}

// Conversation is a DM conversation.
type Conversation struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	Participants    []User   `json:"participants"`
	LastMessage     *Message `json:"lastMessage,omitempty"`
	LastReadEventID string   `json:"lastReadEventId,omitempty"`
	Trusted         bool     `json:"trusted"`
}

// Message is a single DM message.
type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversationId"`
	SenderID       string    `json:"senderId"`
	Text           string    `json:"text"`
	CreatedAt      time.Time `json:"createdAt"`
	MediaURLs      []string  `json:"mediaUrls,omitempty"`
}

// ConversationPage is one page of DM conversations.
type ConversationPage struct {
	Conversations []Conversation `json:"conversations"`
	NextCursor    string         `json:"nextCursor,omitempty"`
	HasNext       bool           `json:"hasNext"`
}

// MessagePage is one page of messages within a conversation.
type MessagePage struct {
	Messages   []Message `json:"messages"`
	NextCursor string    `json:"nextCursor,omitempty"`
	HasNext    bool      `json:"hasNext"`
}

// TweetOption configures CreateTweet, Reply, and QuoteTweet.
type TweetOption func(*tweetOptions)

type tweetOptions struct {
	mediaIDs          []string
	possiblySensitive bool
}

// WithMediaIDs attaches media to a tweet being composed.
func WithMediaIDs(ids ...string) TweetOption {
	return func(o *tweetOptions) { o.mediaIDs = ids }
}

// WithPossiblySensitive flags a tweet's media as possibly sensitive.
func WithPossiblySensitive() TweetOption {
	return func(o *tweetOptions) { o.possiblySensitive = true }
}
