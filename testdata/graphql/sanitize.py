#!/usr/bin/env python3
"""Build sanitized x-go GraphQL fixtures from raw X GraphQL pages.

Usage: sanitize.py <sources.json> <output-dir>

sources.json is kept with the raw pages, outside this repository, and names
the page behind each fixture. File paths are relative to sources.json:

  {"search_timeline_hot.json":    {"file": "a.json", "page": 2, "kind": "search", "select": "features"},
   "search_timeline_first.json":  {"file": "b.json", "page": 0, "kind": "search", "select": "views"},
   "search_timeline_empty.json":  {"file": "c.json", "page": 1, "kind": "search"},
   "tweet_result_by_rest_id.json": {"file": "d.json", "page": 0, "kind": "post"},
   "user_by_screen_name.json":    {"file": "e.json", "page": 0, "kind": "profile"},
   "tweet_detail.json":           {"file": "f.json", "page": 0, "kind": "thread"}}

A raw file is a saved Scarlett x_read result, {"pages": [{"index", "operation",
"body"}]}, where body is X's raw GraphQL response. Entries are chosen by shape,
never by ID: "features" keeps visibility-wrapped and quote posts plus the first
note post and the first media post; "views" keeps up to two posts without a
view count and fills to five; no "select" keeps the page whole.

What changes:
- handles become u<n>; names, text, bios and locations become filler; URLs
  become placeholders; cursors become placeholders;
- every numeric ID (and base64 node IDs such as "User:<id>") is remapped
  consistently across all fixtures;
- created_at values and epoch-millisecond timestamps move back by one random
  offset per run (whole days plus seconds), which is never written anywhere;
- counts of 10 or more (followers, posts, likes, views, ...) and video
  durations are scaled by an independent random factor and rounded to two
  significant figures, the same original value under the same key mapping to
  the same result within a run;
- viewer-relative flags (following, followed_by, blocking, favorited, ...)
  become false; card, tip-jar, affiliate and Birdwatch blocks are dropped.

The check fails the run if any original handle, name, text, ID, timestamp or
user count tuple survives in an output, or if this script's own source holds
one of those values, a UUID or a source path.

tweet_detail_synthetic.json is always written: a TweetDetail body built from
the sanitized post (as the focal post, made a reply) and search posts (as its
two ancestors and its replies, one module holding a tombstone).
"""
import base64
import binascii
import json
import os
import random
import re
import sys
from datetime import datetime, timedelta, timezone

DROP = {"card", "unified_card", "birdwatch_pivot", "tipjar_settings", "affiliates_highlighted_label"}
HANDLE_KEYS = {"screen_name", "in_reply_to_screen_name"}
KEEP_KEYS = {
    "__typename", "type", "entryType", "cursorType", "itemType", "tweetDisplayType", "displayType",
    "direction", "lang", "state", "verified_type", "profile_image_shape", "translator_type",
    "profile_interstitial_type", "withheld_scope", "time_zone", "component", "element", "source",
    "profile_description_language", "status", "reason", "mode", "policy", "professional_type",
    "content_type", "resize", "urlType", "url_type", "userLabelType", "userLabelDisplayType",
    "edits_remaining", "parody_commentary_fan_label",
}
VIEWER_FLAGS = {
    "following", "followed_by", "blocking", "blocked_by", "muting", "follow_request_sent",
    "want_retweets", "notifications", "super_following", "super_followed_by", "favorited",
    "retweeted", "bookmarked",
}
COUNT_STRINGS = {"count", "highlighted_tweets"}
USER_COUNTS = ("followers_count", "friends_count", "statuses_count", "listed_count", "favourites_count", "media_count")
DIGITS = re.compile(r"(?<!\d)\d{6,20}(?!\d)")
NODE_ID = re.compile(r"^([A-Za-z]+):(\d+)$")
UUID = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", re.I)
MAX_SORT_INDEX = "9223372036854775807"
X_DATE = "%a %b %d %H:%M:%S +0000 %Y"


def round2(n):
    """n rounded to two significant figures."""
    if n < 100:
        return n
    scale = 10 ** (len(str(n)) - 2)
    return int(round(n / scale)) * scale


class Sanitizer:
    def __init__(self, rng):
        self.rng = rng
        self.shift = timedelta(days=rng.randint(400, 1500), seconds=rng.randint(0, 86399))
        self.ids = {}
        self.handles = {}
        self.counts = {}
        self.fill = 0
        self.urls = 0
        self.originals = set()
        self.user_counts = []

    def remap_id(self, s):
        if s == MAX_SORT_INDEX:
            return s
        if s not in self.ids:
            n = len(self.ids) + 1
            self.ids[s] = "1" + str(n).zfill(len(s) - 1)
            self.originals.add(s)
        return self.ids[s]

    def remap_digits(self, s):
        return DIGITS.sub(lambda m: self.remap_id(m.group(0)), s)

    def handle(self, h):
        key = h.lower()
        if key not in self.handles:
            self.handles[key] = "u%d" % (len(self.handles) + 1)
            if len(h) >= 4:
                self.originals.add(h)
        return self.handles[key]

    def filler(self, original, prefix="filler text"):
        if len(original) >= 6:
            self.originals.add(original[:24])
        self.fill += 1
        return "%s %d" % (prefix, self.fill)

    def url(self, s):
        self.urls += 1
        if "twimg.com" in s:
            return "https://pbs.twimg.com/media/sanitized%d.jpg" % self.urls
        return "https://example.com/s%d" % self.urls

    def date(self, s):
        t = datetime.strptime(s, X_DATE).replace(tzinfo=timezone.utc)
        self.originals.add(s)
        return (t - self.shift).strftime(X_DATE)

    def epoch_ms(self, s):
        self.originals.add(s)
        return str(int(s) - int(self.shift.total_seconds() * 1000))

    def count(self, key, n):
        """A perturbed count: equal originals under one key agree within a run."""
        if n < 10:
            return n
        if (key, n) not in self.counts:
            out = n
            while out == n:
                out = max(10, round2(int(n * self.rng.uniform(0.6, 1.4))))
            self.counts[(key, n)] = out
        return self.counts[(key, n)]

    def string(self, key, s, parent):
        if s == "":
            return s
        if key in HANDLE_KEYS:
            return self.handle(s)
        if key == "id":
            try:
                decoded = base64.b64decode(s, validate=True).decode()
            except (binascii.Error, UnicodeDecodeError):
                decoded = ""
            m = NODE_ID.match(decoded)
            if m:
                return base64.b64encode(("%s:%s" % (m.group(1), self.remap_id(m.group(2)))).encode()).decode()
        if key == "created_at":
            return self.date(s)
        if (key.endswith("_msecs") or key.endswith("_msec")) and s.isdigit():
            return self.epoch_ms(s)
        if key in COUNT_STRINGS and s.isdigit():
            return str(self.count(key, int(s)))
        if key in KEEP_KEYS:
            return s
        if s.isdigit():
            return self.remap_id(s) if len(s) >= 6 else s
        if key in ("entryId", "entry_id_to_replace", "sortIndex", "media_key") or key.endswith("_str"):
            return self.remap_digits(s)
        if s.startswith("http"):
            return self.url(s)
        if parent == "hashtags":
            return self.filler(s, "tag")
        if key == "value":
            # Cursors encode the IDs of the page's first and last posts.
            return self.filler(s, "cursor")
        return self.filler(s)

    def walk(self, value, key="", parent=""):
        if isinstance(value, dict):
            out = {k: self.walk(v, k, key) for k, v in value.items() if k not in DROP}
            if "followers_count" in value:
                self.user_counts.append((tuple(value.get(k) for k in USER_COUNTS), tuple(out.get(k) for k in USER_COUNTS)))
            return out
        if isinstance(value, list):
            return [self.walk(v, key, parent if isinstance(v, str) else key) for v in value]
        if isinstance(value, bool):
            return False if key in VIEWER_FLAGS else value
        if isinstance(value, int) and (key.endswith("_count") or key == "duration_millis"):
            return self.count(key, value)
        if isinstance(value, str):
            return self.string(key, value, parent)
        return value

    def collect_handles(self, value):
        if isinstance(value, dict):
            for k, v in value.items():
                if k in HANDLE_KEYS and isinstance(v, str) and v:
                    self.handle(v)
                else:
                    self.collect_handles(v)
        elif isinstance(value, list):
            for v in value:
                self.collect_handles(v)

    def check(self, name, body):
        """Fails if any string value in body contains an original value."""
        values = []

        def strings(v):
            if isinstance(v, dict):
                for item in v.values():
                    strings(item)
            elif isinstance(v, list):
                for item in v:
                    strings(item)
            elif isinstance(v, str):
                values.append(v.lower())

        strings(body)
        found = [o for o in self.originals if any(o.lower() in v for v in values)]
        if found:
            raise SystemExit("sanitizer leak in %s: %d original values survive" % (name, len(found)))

    def check_counts(self):
        for original, out in self.user_counts:
            if original == out and any(isinstance(n, int) and n >= 10 for n in original):
                raise SystemExit("sanitizer leak: a user keeps its original counts")

    def check_script(self, paths):
        """Fails if this script holds an original value, a UUID or a directory
        name from a source path."""
        with open(os.path.abspath(__file__), encoding="utf-8") as f:
            source = f.read()
        # Short handles and names also occur as ordinary words, so the script
        # is checked for IDs, timestamps and originals of 8 or more characters.
        lower = source.lower()
        if any(o.lower() in lower for o in self.originals if len(o) >= 8 or o.isdigit()) or UUID.search(source):
            raise SystemExit("sanitizer leak: this script holds an original value or a UUID")
        for path in paths:
            for part in os.path.dirname(os.path.abspath(path)).split(os.sep):
                if len(part) >= 6 and part.lower() in source.lower():
                    raise SystemExit("sanitizer leak: this script names a source directory")


def page_body(path, index):
    with open(path) as f:
        pages = json.load(f)["pages"]
    return json.loads(next(p for p in pages if p["index"] == index)["body"])


def timeline(body):
    return body["data"]["search_by_raw_query"]["search_timeline"]["timeline"]


def unwrap(result):
    return result["tweet"] if result.get("__typename") == "TweetWithVisibilityResults" else result


def post_entries(body):
    for inst in timeline(body)["instructions"]:
        for e in inst.get("entries") or []:
            ic = e["content"].get("itemContent")
            if ic and "tweet_results" in ic:
                yield e, ic["tweet_results"]["result"]


def select_features(body):
    keep, note, media = [], False, False
    for e, r in post_entries(body):
        t = unwrap(r)
        if r.get("__typename") == "TweetWithVisibilityResults" or "quoted_status_result" in t:
            keep.append(e["entryId"])
    for e, r in post_entries(body):
        t = unwrap(r)
        if e["entryId"] in keep:
            continue
        if not note and "note_tweet" in t:
            keep.append(e["entryId"])
            note = True
        elif not media and t.get("legacy", {}).get("extended_entities"):
            keep.append(e["entryId"])
            media = True
    return set(keep)


def select_views(body):
    no_views = [e["entryId"] for e, r in post_entries(body) if "count" not in unwrap(r).get("views", {})]
    keep = set(no_views[:2])
    for e, _ in post_entries(body):
        if len(keep) >= 5:
            break
        keep.add(e["entryId"])
    return keep


def keep_entries(body, keep):
    for inst in timeline(body)["instructions"]:
        if "entries" in inst:
            inst["entries"] = [e for e in inst["entries"] if e["entryId"] in keep or e["entryId"].startswith("cursor-")]
    return body


def tweet_entry(result, entry_id, sort_index):
    return {
        "entryId": entry_id,
        "sortIndex": sort_index,
        "content": {
            "entryType": "TimelineTimelineItem",
            "__typename": "TimelineTimelineItem",
            "itemContent": {
                "itemType": "TimelineTweet",
                "__typename": "TimelineTweet",
                "tweet_results": {"result": result},
                "tweetDisplayType": "Tweet",
            },
        },
    }


def thread_module(module_id, sort_index, results):
    items = []
    for i, result in enumerate(results):
        items.append({
            "entryId": "%s-tweet-%d" % (module_id, i),
            "item": {"itemContent": {
                "itemType": "TimelineTweet",
                "__typename": "TimelineTweet",
                "tweet_results": {"result": result},
                "tweetDisplayType": "Tweet",
            }},
        })
    return {
        "entryId": module_id,
        "sortIndex": sort_index,
        "content": {
            "entryType": "TimelineTimelineModule",
            "__typename": "TimelineTimelineModule",
            "items": items,
            "displayType": "VerticalConversation",
        },
    }


def copy(v):
    return json.loads(json.dumps(v))


def link(result, conversation_id, parent):
    """Makes result a reply to parent (a sanitized post) in conversation_id."""
    t = unwrap(result)
    t["legacy"]["conversation_id_str"] = conversation_id
    if parent is None:
        for k in ("in_reply_to_status_id_str", "in_reply_to_user_id_str", "in_reply_to_screen_name"):
            t["legacy"].pop(k, None)
    else:
        p = unwrap(parent)
        t["legacy"]["in_reply_to_status_id_str"] = p["rest_id"]
        t["legacy"]["in_reply_to_user_id_str"] = p["core"]["user_results"]["result"]["rest_id"]
        author = p["core"]["user_results"]["result"]
        t["legacy"]["in_reply_to_screen_name"] = (author.get("core") or author["legacy"])["screen_name"]
    t.pop("quoted_status_result", None)
    return result


def synthetic_detail(focal, posts):
    """TweetDetail with two ancestors, the focal post, three reply modules (one
    holding a tombstone) and a bottom cursor, in X's current shape."""
    root, parent = copy(posts[0]), copy(posts[1])
    root_id = unwrap(root)["rest_id"]
    link(root, root_id, None)
    link(parent, root_id, root)
    focal = link(copy(focal), root_id, parent)
    replies = [link(copy(r), root_id, focal) for r in posts[2:5]]
    ids = [unwrap(r)["rest_id"] for r in replies]
    tombstone = {"__typename": "TweetTombstone", "tombstone": {"__typename": "TextTombstone", "text": {
        "rtl": False, "text": "This post was deleted by the post author.", "entities": []}}}
    return {"data": {"threaded_conversation_with_injections_v2": {"instructions": [
        {"type": "TimelineClearCache"},
        {"type": "TimelineAddEntries", "entries": [
            tweet_entry(root, "tweet-" + root_id, "7999999999999999999"),
            tweet_entry(parent, "tweet-" + unwrap(parent)["rest_id"], "7999999999999999998"),
            tweet_entry(focal, "tweet-" + unwrap(focal)["rest_id"], "7999999999999999997"),
            thread_module("conversationthread-" + ids[0], "7999999999999999996", replies[0:1]),
            thread_module("conversationthread-" + ids[1], "7999999999999999995", [replies[1], tombstone]),
            thread_module("conversationthread-" + ids[2], "7999999999999999994", replies[2:3]),
            {"entryId": "cursor-bottom-7999999999999999993", "sortIndex": "7999999999999999993", "content": {
                "entryType": "TimelineTimelineItem", "__typename": "TimelineTimelineItem",
                "itemContent": {"itemType": "TimelineTimelineCursor", "__typename": "TimelineTimelineCursor",
                                "value": "sanitized-cursor", "cursorType": "Bottom"}}},
        ]},
        {"type": "TimelineTerminateTimeline", "direction": "Top"},
    ]}}}


def main():
    sources_path, out = sys.argv[1], sys.argv[2]
    with open(sources_path) as f:
        sources = json.load(f)
    base = os.path.dirname(os.path.abspath(sources_path))
    raw, paths = {}, []
    for name in sorted(sources):
        src = sources[name]
        path = os.path.join(base, src["file"])
        paths.append(path)
        body = page_body(path, src.get("page", 0))
        if src.get("select") == "features":
            keep_entries(body, select_features(body))
        elif src.get("select") == "views":
            keep_entries(body, select_views(body))
        raw[name] = body

    s = Sanitizer(random.SystemRandom())
    for body in raw.values():
        s.collect_handles(body)
    fixtures = {name: s.walk(body) for name, body in raw.items()}

    focal = fixtures["tweet_result_by_rest_id.json"]["data"]["tweetResult"]["result"]
    posts = [r for _, r in post_entries(fixtures["search_timeline_hot.json"])]
    fixtures["tweet_detail_synthetic.json"] = synthetic_detail(focal, posts)

    s.check_counts()
    s.check_script(paths)
    for name, body in fixtures.items():
        s.check(name, body)
    for name, body in fixtures.items():
        text = json.dumps(body, ensure_ascii=False, separators=(",", ":"))
        with open(os.path.join(out, name), "w") as f:
            f.write(text + "\n")
        print("%s %d bytes" % (name, len(text)))


if __name__ == "__main__":
    main()
