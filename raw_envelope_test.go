package x

import (
	"encoding/json"
	"testing"
)

func TestTweetResultPreservesUnknownProviderFields(t *testing.T) {
	raw := json.RawMessage(`{
		"__typename":"Tweet",
		"rest_id":"42",
		"legacy":{"full_text":"hello","conversation_id_str":"42"},
		"core":{"user_results":{"result":{"rest_id":"7","legacy":{"screen_name":"author","name":"Author"}}}},
		"card":{"legacy":{"name":"summary","unknown_card_field":{"future":true}}},
		"future_top_level":{"nested":[1,2,3]}
	}`)

	tweet, ok, err := (tweetResult{Result: raw}).tweet()
	if err != nil {
		t.Fatalf("tweet(): %v", err)
	}
	if !ok {
		t.Fatal("tweet() rejected valid result")
	}
	if tweet.ID != "42" || tweet.Text != "hello" || tweet.AuthorID != "7" {
		t.Fatalf("typed projection = %#v", tweet)
	}
	if tweet.Raw == nil || tweet.Raw.SchemaVersion != 1 || tweet.Raw.Provider != "x_graphql" {
		t.Fatalf("raw envelope metadata = %#v", tweet.Raw)
	}

	var preserved map[string]json.RawMessage
	if err := json.Unmarshal(tweet.Raw.Result, &preserved); err != nil {
		t.Fatalf("decode preserved result: %v", err)
	}
	for _, key := range []string{"card", "future_top_level", "legacy", "core"} {
		if _, ok := preserved[key]; !ok {
			t.Errorf("preserved result missing %q", key)
		}
	}
}

func TestTweetResultPreservesVisibilityWrapperAndNestedGraph(t *testing.T) {
	raw := json.RawMessage(`{
		"__typename":"TweetWithVisibilityResults",
		"tweet":{
			"__typename":"Tweet",
			"rest_id":"100",
			"legacy":{
				"full_text":"wrapped",
				"conversation_id_str":"100",
				"retweeted_status_result":{"result":{"rest_id":"90","unknown_repost_field":"kept"}}
			},
			"core":{"user_results":{"result":{"rest_id":"8","legacy":{"screen_name":"wrapped_author","name":"Wrapped Author"}}}},
			"quoted_status_result":{"result":{"rest_id":"80","unknown_quote_field":"kept"}},
			"edit_control":{"edit_tweet_ids":["100"],"editable_until_msecs":"1"},
			"unknown_nested":{"value":"kept"}
		},
		"tweet_interstitial":{"text":{"text":"limited visibility"}},
		"unknown_wrapper_field":true
	}`)

	tweet, ok, err := (tweetResult{Result: raw}).tweet()
	if err != nil {
		t.Fatalf("tweet(): %v", err)
	}
	if !ok {
		t.Fatal("tweet() rejected visibility-wrapped result")
	}
	if tweet.ID != "100" || tweet.Text != "wrapped" || !tweet.IsRetweet {
		t.Fatalf("typed wrapper projection = %#v", tweet)
	}
	if string(tweet.Raw.Result) != string(raw) {
		t.Fatalf("raw result changed:\n got %s\nwant %s", tweet.Raw.Result, raw)
	}
}

func TestParseTweetPageAttachesRawEnvelopeToEntriesAndModules(t *testing.T) {
	raw := json.RawMessage(`{
		"search_by_raw_query":{"search_timeline":{"timeline":{"instructions":[{
			"type":"TimelineAddEntries",
			"entries":[
				{"entryId":"tweet-1","content":{"itemContent":{"tweet_results":{"result":{
					"rest_id":"1","legacy":{"full_text":"entry"},"entry_unknown":"kept"
				}}}}},
				{"entryId":"module-1","content":{"items":[{"item":{"itemContent":{"tweet_results":{"result":{
					"rest_id":"2","legacy":{"full_text":"module"},"module_unknown":"kept"
				}}}}}]}}
			]
		}]}}}
	}`)

	page, err := parseTweetPage(raw, "search_by_raw_query.search_timeline")
	if err != nil {
		t.Fatalf("parseTweetPage: %v", err)
	}
	if len(page.Tweets) != 2 {
		t.Fatalf("len(Tweets) = %d, want 2", len(page.Tweets))
	}
	for i, key := range []string{"entry_unknown", "module_unknown"} {
		if page.Tweets[i].Raw == nil {
			t.Fatalf("tweet %d missing raw envelope", i)
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(page.Tweets[i].Raw.Result, &result); err != nil {
			t.Fatalf("decode tweet %d: %v", i, err)
		}
		if _, ok := result[key]; !ok {
			t.Errorf("tweet %d missing unknown field %q", i, key)
		}
	}
}
