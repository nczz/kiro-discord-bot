package discordmember

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestSplitNamesHandlesNaturalSeparators(t *testing.T) {
	got := SplitNames("Wendy、Cheisy 跟 Wendy\nAlice")
	want := []string{"Wendy", "Cheisy", "Alice"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("names = %#v, want %#v", got, want)
	}
}

func TestValidateNamesRejectsRawIDs(t *testing.T) {
	for _, names := range [][]string{
		{"123456789012345678"},
		{"<@123456789012345678>"},
		{"<@!123456789012345678>"},
	} {
		if err := ValidateNames(names); err == nil {
			t.Fatalf("ValidateNames(%+v) accepted raw mention ID", names)
		}
	}
	if err := ValidateNames([]string{"Wendy Chen", "Cheisy"}); err != nil {
		t.Fatalf("validate display names: %v", err)
	}
}

func TestResolveNameAllowsUniqueFreshPrefix(t *testing.T) {
	member := &discordgo.Member{Nick: "Wendy Chen", User: &discordgo.User{ID: "123", Username: "wendy", GlobalName: "Wendy Chen"}}
	got, ambiguous, ok := ResolveName("Wendy", "fresh_search_unique", []*discordgo.Member{member})
	if !ok {
		t.Fatalf("resolve failed ambiguous=%+v", ambiguous)
	}
	if got.UserID != "123" || got.Placeholder != "[[discord:user:123]]" {
		t.Fatalf("resolved = %+v", got)
	}
}

func TestAmbiguousMentionCandidatesDoNotExposeReusableUserIDs(t *testing.T) {
	members := []*discordgo.Member{
		{Nick: "Wendy Chen", User: &discordgo.User{ID: "123", Username: "wendychen"}},
		{Nick: "Wendy Lin", User: &discordgo.User{ID: "456", Username: "wendylin"}},
	}
	_, ambiguous, ok := ResolveName("Wendy", "fresh_lookup", members)
	if ok || ambiguous.Query != "Wendy" || len(ambiguous.Candidates) != 2 {
		t.Fatalf("ambiguous = %+v, resolved=%v", ambiguous, ok)
	}
	raw, err := json.Marshal(ambiguous)
	if err != nil {
		t.Fatalf("marshal ambiguous candidates: %v", err)
	}
	if strings.Contains(string(raw), "user_id") || strings.Contains(string(raw), "123") || strings.Contains(string(raw), "456") {
		t.Fatalf("ambiguous candidates leaked reusable user IDs: %s", raw)
	}
}

type resolverRoundTripFunc func(*http.Request) (*http.Response, error)

func (f resolverRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestScanGuildMembersHonorsScannedMemberBound(t *testing.T) {
	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new Discord session: %v", err)
	}
	requests := 0
	session.Client.Transport = resolverRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		if requests > 1 {
			t.Fatalf("member scan exceeded bound with request %d: %s", requests, req.URL.String())
		}
		if req.URL.Query().Get("limit") != "2" {
			t.Fatalf("member scan limit = %q, want 2", req.URL.Query().Get("limit"))
		}
		body := `[
			{"user":{"id":"100","username":"alice"}},
			{"user":{"id":"200","username":"bob"}}
		]`
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	got, err := scanGuildMembers(session, "guild-1", 2)
	if err != nil {
		t.Fatalf("scan guild members: %v", err)
	}
	if len(got) != 2 || requests != 1 {
		t.Fatalf("scan result = %+v, requests=%d; want two members in one bounded request", got, requests)
	}
}

func TestLookupMembersScansBeforeResolvingSearchResult(t *testing.T) {
	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new Discord session: %v", err)
	}
	session.Client.Transport = resolverRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := ""
		switch {
		case strings.HasSuffix(req.URL.Path, "/members/search"):
			body = `[{"nick":"Wendy Helper","user":{"id":"100","username":"wendy-helper"}}]`
		case strings.HasSuffix(req.URL.Path, "/members"):
			body = `[{"user":{"id":"200","username":"other","global_name":"Wendy"}}]`
		default:
			t.Fatalf("unexpected Discord request: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})

	resolved, err := ResolveNames(session, "guild-1", []string{"Wendy"}, 10)
	if err != nil {
		t.Fatalf("resolve names: %v", err)
	}
	if len(resolved.Resolved) != 1 || resolved.Resolved[0].UserID != "200" || len(resolved.Ambiguous) != 0 {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestResolveNamesReturnsDiscordLookupFailure(t *testing.T) {
	session, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatalf("new Discord session: %v", err)
	}
	session.Client.Transport = resolverRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("member endpoint unavailable")
	})

	_, err = ResolveNames(session, "guild-1", []string{"Wendy"}, 10)
	if err == nil || !strings.Contains(err.Error(), "member endpoint unavailable") {
		t.Fatalf("lookup error = %v", err)
	}
}
