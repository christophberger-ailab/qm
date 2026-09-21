package web

// copilot_test.go — the GitHub Copilot connection kind.
//
// What can be tested here is everything up to the child process: that a
// connection of this kind is stored and offered without the base URL it
// has no use for, and that an answer is read out of the session event it
// arrives in. Starting the Copilot CLI itself is not something a unit
// test does, so the run is not exercised end to end.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

// A Copilot connection is a name and a model. There is no address to give
// -- the CLI knows where GitHub is -- so the base URL the other two kinds
// insist on is not asked for, and one entered anyway is dropped rather
// than stored as a setting that does nothing.
func TestACopilotConnectionNeedsNoBaseURL(t *testing.T) {
	srv, _ := configTestServer(t)
	addConnection(t, srv, "Copilot", "copilot", "", "gpt-5", "")

	got := srv.cfg.Copyedit.Connections[0]
	if got.Kind != kindCopilot {
		t.Fatalf("kind = %q, want %q", got.Kind, kindCopilot)
	}
	if got.BaseURL != "" {
		t.Errorf("baseURL = %q, want it empty", got.BaseURL)
	}

	rec := post(t, srv, "/config/connections", url.Values{
		"id": {"c1"}, "name": {"Copilot"}, "kind": {"copilot"},
		"base_url": {"https://api.github.com"}, "model": {"gpt-5"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := srv.cfg.Copyedit.Connections[0].BaseURL; got != "" {
		t.Errorf("baseURL = %q, want a URL entered against Copilot to be dropped", got)
	}
}

// The other two kinds are still held to their address.
func TestTheHTTPKindsStillNeedABaseURL(t *testing.T) {
	srv, _ := configTestServer(t)
	rec := post(t, srv, "/config/connections", url.Values{
		"name": {"Local"}, "kind": {"openai"}, "base_url": {""}, "model": {"llama"},
	})
	if !strings.Contains(rec.Body.String(), "needs a base URL") {
		t.Fatalf("a connection without a base URL was accepted:\n%s", rec.Body)
	}
	if len(srv.cfg.Copyedit.Connections) != 0 {
		t.Fatalf("it was stored anyway: %+v", srv.cfg.Copyedit.Connections)
	}
}

// A kind that names none of the three is taken as the OpenAI-style one,
// that being what an unknown provider most likely speaks.
func TestAnUnknownKindIsTakenAsOpenAI(t *testing.T) {
	srv, _ := configTestServer(t)
	addConnection(t, srv, "Whatever", "gemini", "https://x/v1", "m", "")
	if got := srv.cfg.Copyedit.Connections[0].Kind; got != kindOpenAI {
		t.Fatalf("kind = %q, want %q", got, kindOpenAI)
	}
}

// The connections page offers the kind, and says of a stored one that it
// wants no base URL.
func TestTheConnectionsPageOffersCopilot(t *testing.T) {
	srv, _ := configTestServer(t)
	addConnection(t, srv, "Copilot", "copilot", "", "gpt-5", "")
	body := get(t, srv, "/config/connections").Body.String()
	for _, want := range []string{`value="copilot"`, "GitHub Copilot", "COPILOT_CLI_PATH"} {
		if !strings.Contains(body, want) {
			t.Errorf("connections page missing %q", want)
		}
	}
	if !strings.Contains(body, `class="connection-base-url" hidden`) {
		t.Errorf("the base URL field is shown for a Copilot connection:\n%s", body)
	}
}

// Copilot is not an endpoint, so nothing about it goes through the HTTP
// path: requestFor is never reached for it, and a connection of this kind
// carries no URL for it to compose.
func TestTheCopilotKindIsNotAnEndpoint(t *testing.T) {
	if needsEndpoint(kindCopilot) {
		t.Error("the Copilot kind is asked for a base URL")
	}
	for _, kind := range []string{kindAnthropic, kindOpenAI} {
		if !needsEndpoint(kind) {
			t.Errorf("the %s kind is not asked for a base URL", kind)
		}
	}
}

// A connection naming no model is refused before the CLI is started: on
// this kind the model is the only thing the session cannot be given a
// default for.
func TestACopilotConnectionWithoutAModelDoesNotStartTheCLI(t *testing.T) {
	_, err := askModel(apiConnection{Name: "Copilot", Kind: kindCopilot}, "s", "p", "t", 1)
	if err == nil || !strings.Contains(err.Error(), "names no model") {
		t.Fatalf("err = %v, want it to name the missing model", err)
	}
}

func TestCopilotAnswerReadsTheAssistantMessage(t *testing.T) {
	reasoning := "  {\"suggestions\":[]}  "
	cases := map[string]struct {
		event *copilot.SessionEvent
		want  string
		err   string
	}{
		"the content": {
			event: &copilot.SessionEvent{Data: &copilot.AssistantMessageData{Content: `{"suggestions":[]}`}},
			want:  `{"suggestions":[]}`,
		},
		// A model that thought its way to the answer and never wrote a
		// final message is taken at its reasoning, as it is on the two
		// HTTP kinds.
		"the reasoning, when there is no content": {
			event: &copilot.SessionEvent{Data: &copilot.AssistantMessageData{ReasoningText: &reasoning}},
			want:  reasoning,
		},
		"nothing at all": {
			event: &copilot.SessionEvent{Data: &copilot.AssistantMessageData{}},
			err:   "no text",
		},
		"no message": {
			event: nil,
			err:   "without writing an answer",
		},
		"some other event": {
			event: &copilot.SessionEvent{Data: &copilot.SessionIdleData{}},
			err:   "rather than an answer",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := copilotAnswer(c.event)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want it to mention %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("answer = %q, want %q", got, c.want)
			}
		})
	}
}

// The session is created without tools, so nothing should ever ask; if
// something does, the answer is no. A copyedit run is not the thing that
// writes to the user's disk.
func TestTheCopilotSessionRefusesEveryTool(t *testing.T) {
	decision, err := refuseEverything(nil, copilot.PermissionInvocation{})
	if err != nil {
		t.Fatal(err)
	}
	reject, ok := decision.(*rpc.PermissionDecisionReject)
	if !ok {
		t.Fatalf("decision = %T, want a rejection", decision)
	}
	if reject.Feedback == nil || !strings.Contains(*reject.Feedback, "no tools") {
		t.Errorf("the model is not told why it was refused: %v", reject.Feedback)
	}
}
