package web

import (
	"net/url"
	"strings"
	"testing"
)

// The name comments are signed with is one setting, set on its own config
// page and kept in the settings file; left empty, the system's login name
// stands in for it (spec-web.yaml SETTINGS 2, UI 3-8-6).
func TestUserNameSignsComments(t *testing.T) {
	prev := systemUserName
	systemUserName = func() string { return "login" }
	t.Cleanup(func() { systemUserName = prev })

	srv, _ := configTestServer(t)
	if body := get(t, srv, "/").Body.String(); !strings.Contains(body, `<body data-user="login">`) {
		t.Errorf("without a name the page does not carry the login name:\n%.400s", body)
	}
	if body := get(t, srv, "/config").Body.String(); !strings.Contains(body, `href="/config/user"`) {
		t.Errorf("config page does not link the name setting:\n%s", body)
	}
	if body := get(t, srv, "/config/user").Body.String(); !strings.Contains(body, `placeholder="login"`) {
		t.Errorf("name page does not offer the login name:\n%s", body)
	}

	body := post(t, srv, "/config/user", url.Values{"name": {"  Ana Autor "}}).Body.String()
	if !strings.Contains(body, "Saved.") {
		t.Fatalf("not saved:\n%s", body)
	}
	again, err := newServer(srv.configFile)
	if err != nil {
		t.Fatal(err)
	}
	if again.cfg.User.Name != "Ana Autor" {
		t.Errorf("restored name = %q", again.cfg.User.Name)
	}
	if body := get(t, srv, "/").Body.String(); !strings.Contains(body, `<body data-user="Ana Autor">`) {
		t.Errorf("the page does not carry the configured name:\n%.400s", body)
	}

	body = post(t, srv, "/config/user", url.Values{"name": {`Ana "the" Autor`}}).Body.String()
	if !strings.Contains(body, "cannot contain") || srv.cfg.User.Name != "Ana Autor" {
		t.Errorf("a name that breaks the attribute block was stored: %q\n%s", srv.cfg.User.Name, body)
	}

	post(t, srv, "/config/user", url.Values{"name": {""}})
	if body := get(t, srv, "/").Body.String(); !strings.Contains(body, `<body data-user="login">`) {
		t.Errorf("clearing the name does not fall back to the login name:\n%.400s", body)
	}
}
