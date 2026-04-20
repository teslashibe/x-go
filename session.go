package x

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// validateSession calls the Viewer query to verify auth and cache the
// authenticated user's profile.
func (c *Client) validateSession(ctx context.Context) error {
	vars := map[string]interface{}{
		"withCommunitiesMemberships": true,
		"withSubscribedTab":          true,
		"withCommunitiesCreation":    true,
	}

	raw, err := c.graphqlGET(ctx, "Viewer", vars)
	if err != nil {
		return fmt.Errorf("%w: session validation failed: %v", ErrUnauthorized, err)
	}

	var data struct {
		Viewer struct {
			UserResults struct {
				Result userObj `json:"result"`
			} `json:"user_results"`
		} `json:"viewer"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("%w: decoding viewer: %v", ErrUnauthorized, err)
	}

	if data.Viewer.UserResults.Result.RestID == "" {
		return ErrUnauthorized
	}

	u := toUser(data.Viewer.UserResults.Result)
	c.viewer = &u

	if c.restID == "" {
		c.restID = u.ID
	}

	return nil
}

// Regex patterns for extracting queryIDs from X's main.js bundle.
var (
	reMainJS  = regexp.MustCompile(`"([^"]*main\.[a-f0-9]+[a-z]\.js)"`)
	reQueryID = regexp.MustCompile(`\{queryId:"([^"]+)",operationName:"([^"]+)",operationType:"([^"]+)"`)
)

// RefreshQueryIDs fetches the current main.js bundle from x.com and extracts
// all queryId/operationName pairs, updating the client's queryIDs map under
// a write lock.
func (c *Client) RefreshQueryIDs(ctx context.Context) error {
	htmlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return fmt.Errorf("%w: building HTML request: %v", ErrRequestFailed, err)
	}
	htmlReq.Header.Set("User-Agent", c.userAgent)
	htmlReq.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	htmlResp, err := c.httpClient.Do(htmlReq)
	if err != nil {
		return fmt.Errorf("%w: fetching x.com: %v", ErrRequestFailed, err)
	}
	defer htmlResp.Body.Close()

	htmlBody, err := io.ReadAll(htmlResp.Body)
	if err != nil {
		return fmt.Errorf("%w: reading HTML: %v", ErrRequestFailed, err)
	}

	matches := reMainJS.FindSubmatch(htmlBody)
	if matches == nil {
		return fmt.Errorf("%w: could not find main.js URL in page source", ErrRequestFailed)
	}

	jsURL := string(matches[1])
	if strings.HasPrefix(jsURL, "//") {
		jsURL = "https:" + jsURL
	} else if jsURL != "" && jsURL[0] == '/' {
		jsURL = baseURL + jsURL
	}

	jsReq, err := http.NewRequestWithContext(ctx, http.MethodGet, jsURL, nil)
	if err != nil {
		return fmt.Errorf("%w: building JS request: %v", ErrRequestFailed, err)
	}
	jsReq.Header.Set("User-Agent", c.userAgent)

	jsResp, err := c.httpClient.Do(jsReq)
	if err != nil {
		return fmt.Errorf("%w: fetching main.js: %v", ErrRequestFailed, err)
	}
	defer jsResp.Body.Close()

	jsBody, err := io.ReadAll(jsResp.Body)
	if err != nil {
		return fmt.Errorf("%w: reading main.js: %v", ErrRequestFailed, err)
	}

	found := reQueryID.FindAllSubmatch(jsBody, -1)
	if len(found) == 0 {
		return fmt.Errorf("%w: no queryIds found in main.js", ErrRequestFailed)
	}

	c.reqMu.Lock()
	for _, m := range found {
		opName := string(m[2])
		qid := string(m[1])
		c.queryIDs[opName] = qid
	}
	c.reqMu.Unlock()

	return nil
}
