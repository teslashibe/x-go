// Package mcp exposes the x-go [x.Client] surface as a set of MCP (Model
// Context Protocol) tools that any host application can mount on its own
// MCP server.
//
// All tools wrap exported methods on *x.Client. Each tool is defined via
// [mcptool.Define] so the JSON input schema is reflected from the typed
// input struct — no hand-maintained schemas, no drift.
//
// Usage from a host application:
//
//	import (
//	    "github.com/teslashibe/mcptool"
//	    x "github.com/teslashibe/x-go"
//	    xmcp "github.com/teslashibe/x-go/mcp"
//	)
//
//	client, _ := x.New(x.Cookies{...})
//	for _, tool := range (xmcp.Provider{}).Tools() {
//	    // register tool with your MCP server, passing client as the client
//	    // argument when invoking
//	}
//
// The [Excluded] map documents methods on *Client that are intentionally
// not exposed via MCP, with a one-line reason. The coverage test in
// mcp_test.go fails if a new exported method is added without either being
// wrapped by a tool or appearing in [Excluded].
package mcp

import "github.com/teslashibe/mcptool"

// Provider implements [mcptool.Provider] for x-go. The zero value is ready
// to use.
type Provider struct{}

// Platform returns "x".
func (Provider) Platform() string { return "x" }

// Tools returns every x-go MCP tool, in registration order.
func (Provider) Tools() []mcptool.Tool {
	out := make([]mcptool.Tool, 0,
		len(userTools)+
			len(tweetTools)+
			len(timelineTools)+
			len(searchTools)+
			len(composeTools)+
			len(actionTools)+
			len(socialTools)+
			len(dmTools)+
			len(listTools)+
			len(trendTools),
	)
	out = append(out, userTools...)
	out = append(out, tweetTools...)
	out = append(out, timelineTools...)
	out = append(out, searchTools...)
	out = append(out, composeTools...)
	out = append(out, actionTools...)
	out = append(out, socialTools...)
	out = append(out, dmTools...)
	out = append(out, listTools...)
	out = append(out, trendTools...)
	return out
}
