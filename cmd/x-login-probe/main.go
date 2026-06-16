// Command x-login-probe smoke-tests X username/password login end to end: it
// logs in via the onboarding flow, mints auth_token + ct0, builds a client, and
// calls Me() for liveness. Credentials come from env:
//
//	X_USERNAME, X_PASSWORD, X_TOTP_SECRET (optional), X_OTP (optional one-time),
//	X_PROXY_URL (optional residential egress)
//
// Exit 0 on success (prints the authenticated handle), non-zero otherwise.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	x "github.com/teslashibe/x-go"
)

func main() {
	user := strings.TrimSpace(os.Getenv("X_USERNAME"))
	pass := os.Getenv("X_PASSWORD")
	if user == "" || pass == "" {
		fmt.Fprintln(os.Stderr, "x-login-probe: set X_USERNAME and X_PASSWORD")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	res, err := x.Login(ctx, x.LoginParams{
		Username:   user,
		Password:   pass,
		TOTPSecret: strings.TrimSpace(os.Getenv("X_TOTP_SECRET")),
		OTP:        strings.TrimSpace(os.Getenv("X_OTP")),
		ProxyURL:   strings.TrimSpace(os.Getenv("X_PROXY_URL")),
		InitialCookieHeader: strings.TrimSpace(os.Getenv("X_INITIAL_COOKIE_HEADER")),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "login: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("login ok: auth_token_len=%d ct0_len=%d twid=%s\n",
		len(res.Cookies.AuthToken), len(res.Cookies.CT0), res.Cookies.Twid)

	c, err := x.New(res.Cookies)
	if err != nil {
		fmt.Fprintf(os.Stderr, "new client: %v\n", err)
		os.Exit(1)
	}
	me, err := c.Me(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Me(): %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("liveness ok: @%s (id=%s)\n", me.ScreenName, me.ID)
}
