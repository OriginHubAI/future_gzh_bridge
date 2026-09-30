package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"gkx/wcplus/internal/bridgeclient"
)

func main() {
	bc := bridgeclient.NewFromEnv()
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "wcplus-bridge",
		Version: "1.0.0",
	}, nil)

	registerTools(server, bc)

	log.Printf("[wcplus-mcp] bridge=%s tools=7 (stdio)", bc.BaseURL())

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

func registerTools(server *mcp.Server, bc *bridgeclient.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "wcplus_initialize",
		Description: "Check wcplus license, login status, and recommended next actions (software initialization).",
	}, tool(bc, func(ctx context.Context) (any, error) {
		return bcPost(ctx, bc, "/v1/rpc/initialize", map[string]any{})
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wcplus_login_status",
		Description: "Get Max status, license, WeChat params hint, needsManualLogin.",
	}, tool(bc, func(ctx context.Context) (any, error) {
		return bcGet(ctx, bc, "/v1/rpc/login-status", nil)
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wcplus_login_prepare",
		Description: "Start login forwarding: wcplus Set Proxy. User must open target article in WeChat PC client unless WeChat Auto is used.",
	}, toolWithIn(bc, func(ctx context.Context, in loginPrepareIn) (any, error) {
		body := map[string]any{}
		if in.ArticleURL != "" {
			body["articleURL"] = in.ArticleURL
		}
		return bcPost(ctx, bc, "/v1/rpc/login/prepare", body)
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wcplus_login_finish",
		Description: "End login forwarding: Clear Proxy and refresh login status.",
	}, tool(bc, func(ctx context.Context) (any, error) {
		return bcPost(ctx, bc, "/v1/rpc/login/finish", map[string]any{})
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wcplus_import_official_account",
		Description: "Import a WeChat official account by exact nickname or biz; creates link task in wcplus.",
	}, toolWithIn(bc, func(ctx context.Context, in importIn) (any, error) {
		return bcPost(ctx, bc, "/v1/rpc/import-official-account", in)
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wcplus_sync_official_account",
		Description: "Sync official account (link/article steps). Optional waitQueue and exportAfter for sync then export latest.",
	}, toolWithIn(bc, func(ctx context.Context, in syncIn) (any, error) {
		return bcPost(ctx, bc, "/v1/rpc/sync-official-account", in)
	}))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "wcplus_export_latest_articles",
		Description: "Export latest N articles for a biz; optional full content per article.",
	}, toolWithIn(bc, func(ctx context.Context, in exportIn) (any, error) {
		q := url.Values{}
		q.Set("biz", in.Biz)
		if in.Nickname != "" {
			q.Set("nickname", in.Nickname)
		}
		if in.Limit > 0 {
			q.Set("limit", fmt.Sprintf("%d", in.Limit))
		}
		if in.WithContent {
			q.Set("withContent", "true")
		}
		return bcGet(ctx, bc, "/v1/rpc/export-latest-articles", q)
	}))
}

type loginPrepareIn struct {
	ArticleURL string `json:"articleURL,omitempty"`
}

type importIn struct {
	Biz      string `json:"biz,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	RunQueue *bool  `json:"runQueue,omitempty"`
}

type syncIn struct {
	Biz            string   `json:"biz"`
	Nickname       string   `json:"nickname"`
	Steps          []string `json:"steps,omitempty"`
	RunQueue       *bool    `json:"runQueue,omitempty"`
	WaitQueue      bool     `json:"waitQueue,omitempty"`
	WaitTimeoutSec int      `json:"waitTimeoutSec,omitempty"`
	ExportAfter    bool     `json:"exportAfter,omitempty"`
	ExportLimit    int      `json:"exportLimit,omitempty"`
	WithContent    bool     `json:"withContent,omitempty"`
}

type exportIn struct {
	Biz         string `json:"biz"`
	Nickname    string `json:"nickname,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	WithContent bool   `json:"withContent,omitempty"`
}

func tool(bc *bridgeclient.Client, fn func(context.Context) (any, error)) func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return runTool(ctx, fn)
	}
}

func toolWithIn[T any](bc *bridgeclient.Client, fn func(context.Context, T) (any, error)) func(context.Context, *mcp.CallToolRequest, T) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in T) (*mcp.CallToolResult, any, error) {
		return runTool(ctx, func(ctx context.Context) (any, error) {
			return fn(ctx, in)
		})
	}
}

func runTool(ctx context.Context, fn func(context.Context) (any, error)) (*mcp.CallToolResult, any, error) {
	out, err := fn(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			IsError: true,
		}, nil, nil
	}
	text, _ := json.MarshalIndent(out, "", "  ")
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(text)}},
	}, out, nil
}

func bcGet(ctx context.Context, bc *bridgeclient.Client, path string, q url.Values) (json.RawMessage, error) {
	return bc.Get(ctx, path, q)
}

func bcPost(ctx context.Context, bc *bridgeclient.Client, path string, body any) (json.RawMessage, error) {
	return bc.PostJSON(ctx, path, body)
}
