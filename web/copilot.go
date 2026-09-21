package web

// copilot.go — the GitHub Copilot connection kind.
//
// The other two kinds are endpoints: a URL, a key in a header, JSON in
// and JSON out (see llm.go). Copilot is not one. It is reached through
// the GitHub Copilot CLI, which qm starts as a child process and talks to
// over the CLI's own protocol; the SDK that speaks that protocol is
// vendored because, unlike a chat completion, there is nothing here that
// could sensibly be written by hand.
//
// What that buys is that a connection needs neither an address nor a key:
// the CLI knows where GitHub is and who the user is, so a Copilot
// connection is a name and a model. What it costs is a process. The CLI
// has to be installed and signed in, and every run starts one and stops
// it again -- a second or so before the model is even asked.
//
// The agent is shut down to a chat. Copilot is built to read files, run
// commands and edit a repository, and a copyedit run wants none of that:
// it wants one page read and one JSON answer written. So the client runs
// in ModeEmpty, which starts a session with no tools at all, the system
// message is *replaced* by the same instruction the other two kinds get
// rather than appended to the CLI's own persona, and a permission handler
// refuses anything that asks anyway. Nothing in the user's project is
// reachable from the session: it is given a working directory of its own,
// an empty temporary one, which goes when the run does.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

// copilotCLI is the Copilot CLI qm starts: the one COPILOT_CLI_PATH names,
// or the one on PATH.
//
// The path is resolved here and handed to the SDK rather than left to it.
// The SDK's own default is a runtime it expects to have been bundled into
// the binary at build time (`go tool bundler`), and failing that it does
// not look on PATH at all -- it reports a missing bundle, which is a
// sentence about our build, not about the user's machine. qm ships no
// bundle: the CLI is the user's own installation, signed in as the user,
// and that is the thing to find and to name when it is not there.
func copilotCLI() (string, error) {
	if path := strings.TrimSpace(os.Getenv("COPILOT_CLI_PATH")); path != "" {
		return path, nil
	}
	path, err := exec.LookPath("copilot")
	if err != nil {
		return "", errors.New("the GitHub Copilot CLI was not found: install it and put `copilot` on your PATH, " +
			"or name the executable in COPILOT_CLI_PATH")
	}
	return path, nil
}

// copilotHome is the directory the Copilot CLI keeps its own data in:
// COPILOT_HOME, or ~/.copilot, which is where the CLI itself looks.
//
// qm has to name it because ModeEmpty insists on being told where session
// state goes, and it has to name the real one rather than a scratch
// directory because that is also where the CLI keeps the user's sign-in.
// A session pointed elsewhere is a session with no credentials, and the
// run fails on authentication rather than on anything the user did.
func copilotHome() (string, error) {
	if home := strings.TrimSpace(os.Getenv("COPILOT_HOME")); home != "" {
		return home, nil
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the Copilot CLI's home directory: %w", err)
	}
	return filepath.Join(dir, ".copilot"), nil
}

// askCopilot puts one copyedit question to Copilot and returns the
// model's text, in the same shape askModel returns it for the HTTP kinds,
// so decodeSuggestions and locateAll do not know the difference.
//
// The system, page and tasks blocks arrive apart because the HTTP kinds
// keep them apart for the sake of prompt caching. Here there is nothing
// to cache across runs -- a session is created and thrown away -- so the
// page and the tasks are joined into the one message, in that order, and
// the system block becomes the session's system message.
func askCopilot(conn apiConnection, system, page, tasks string) (string, error) {
	cli, err := copilotCLI()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()

	home, err := copilotHome()
	if err != nil {
		return "", err
	}
	// An empty directory for the session to sit in. It is the session's
	// working directory and nothing else: the agent's idea of "here" is
	// then a folder with nothing in it rather than the book being
	// edited, so even a tool it should not have could reach nothing of
	// the user's. It goes when the run does.
	scratch, err := os.MkdirTemp("", "qm-copyedit-")
	if err != nil {
		return "", fmt.Errorf("making a scratch directory for the Copilot session: %w", err)
	}
	defer os.RemoveAll(scratch)

	options := &copilot.ClientOptions{
		Connection: copilot.StdioConnection{Path: cli},
		// ModeEmpty is the sandboxed surface: no built-in tools unless
		// asked for by name, no environment context in the system
		// message, no custom instructions from the filesystem, telemetry
		// off. A copy editor needs none of them.
		Mode:             copilot.ModeEmpty,
		BaseDirectory:    home,
		WorkingDirectory: scratch,
		LogLevel:         "error",
	}
	// The key field is optional on this kind: empty means "whoever the
	// CLI is signed in as", which is the usual case, and a token in it
	// means that account instead. Setting GitHubToken at all turns the
	// logged-in user off, so it is only set when there is one.
	if token := strings.TrimSpace(conn.Key); token != "" {
		options.GitHubToken = token
	}

	client := copilot.NewClient(options)
	if err := client.Start(ctx); err != nil {
		return "", fmt.Errorf("starting the GitHub Copilot CLI (%s): %w "+
			"(it must be signed in — try running `copilot` once)", cli, err)
	}
	defer client.Stop()

	session, err := client.CreateSession(ctx, &copilot.SessionConfig{
		ClientName: "qm",
		Model:      conn.Model,
		// The instruction replaces the CLI's coding-agent persona
		// outright. Appended, it would be advice to a programmer about
		// to edit a repository; replacing it leaves a copy editor with
		// one page and one task.
		SystemMessage: &copilot.SystemMessageConfig{
			Mode:    "replace",
			Content: system,
		},
		WorkingDirectory: scratch,
		// ModeEmpty makes every session say which tools it wants. This
		// one wants none: it is handed the page and asked for JSON, and
		// there is nothing on the machine it has any business reading.
		// The list is empty rather than absent, the two meaning
		// different things here -- absent is the omission ModeEmpty
		// refuses, empty is the answer "no tools".
		AvailableTools:      []string{},
		OnPermissionRequest: refuseEverything,
		// Nothing to compact: a session lives for one question.
		InfiniteSessions: &copilot.InfiniteSessionConfig{Enabled: copilot.Bool(false)},
		Streaming:        copilot.Bool(false),
	})
	if err != nil {
		return "", fmt.Errorf("%s could not open a Copilot session for %q: %w", conn.Name, conn.Model, err)
	}
	defer session.Disconnect()

	event, err := session.SendAndWait(ctx, copilot.MessageOptions{Prompt: page + "\n\n" + tasks})
	if err != nil {
		return "", fmt.Errorf("%s: %w", conn.Name, err)
	}
	return copilotAnswer(event)
}

// refuseEverything is the session's answer to any request to use a tool.
// There should be none -- the session is created without tools -- so this
// is the second lock rather than the first: if a future runtime offers
// the agent something anyway, a copyedit run is still not the thing that
// writes to the user's disk. The feedback tells the model why, so that it
// goes back to answering rather than retrying.
func refuseEverything(request copilot.PermissionRequest, invocation copilot.PermissionInvocation) (rpc.PermissionDecision, error) {
	feedback := "This is a copyediting session with no tools. Answer with the JSON you were asked for."
	return &rpc.PermissionDecisionReject{Feedback: &feedback}, nil
}

// copilotAnswer digs the text out of the event SendAndWait returned, and
// says what came back when none of it was text -- the same courtesy
// noTextError does for the HTTP kinds, and for the same reason: "no
// answer" is not something a user can act on.
func copilotAnswer(event *copilot.SessionEvent) (string, error) {
	if event == nil {
		return "", errors.New("Copilot finished without writing an answer")
	}
	message, ok := event.Data.(*copilot.AssistantMessageData)
	if !ok {
		return "", fmt.Errorf("Copilot finished with a %s rather than an answer", event.Type())
	}
	if text := strings.TrimSpace(message.Content); text != "" {
		return message.Content, nil
	}
	// A reasoning model that spent the turn thinking and never wrote a
	// final message is taken at its reasoning: the JSON asked for is
	// often in there, which beats refusing an answer the model gave.
	if message.ReasoningText != nil && strings.TrimSpace(*message.ReasoningText) != "" {
		return *message.ReasoningText, nil
	}
	return "", errors.New("Copilot answered with no text")
}
