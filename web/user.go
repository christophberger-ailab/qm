package web

// user.go — who is writing.
//
// qm web runs on the user's own machine, so there is no login: the one
// person at the keyboard is the user, and all the app needs to know about
// them is the name their comments are signed with. That name is one
// setting, `user: name:` in the settings file; left empty, the name the
// user is logged in to the computer with stands in for it.

import (
	"net/http"
	"os/user"
	"strings"
)

// userConfig is who the comments written in the editor are by.
type userConfig struct {
	Name string `json:"name"`
}

// commentAuthor is the name a new comment or reply is signed with: the
// configured name, or the system's login name when none is configured.
// It is "" only when neither is known, and the comment then goes unsigned.
func (s *server) commentAuthor() string {
	if name := strings.TrimSpace(s.cfg.User.Name); name != "" {
		return name
	}
	return systemUserName()
}

// systemUserName is the login name of whoever runs qm, without the domain
// Windows puts in front of it.
var systemUserName = func() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	name := u.Username
	if i := strings.LastIndex(name, `\`); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// userPageView is the page data for /config/user.
type userPageView struct {
	// Name is the configured name, empty when the system's is used;
	// Fallback is the system's.
	Name     string
	Fallback string
	Message  string
	Error    string
}

func (s *server) userPageView() userPageView {
	return userPageView{Name: s.cfg.User.Name, Fallback: systemUserName()}
}

func (s *server) userConfigPage(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.render(w, "user-page", s.userPageView())
}

// saveUserHandler stores the name comments are signed with. An empty name
// is a choice too: it hands the signing back to the system's login name.
// A double quote is refused, since the name goes into a comment's
// attribute block between double quotes, and so is a line break.
func (s *server) saveUserHandler(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ParseForm()
	view := s.userPageView()
	name := strings.TrimSpace(r.PostFormValue("name"))
	view.Name = name
	if strings.ContainsAny(name, "\"\r\n{}") {
		view.Error = "A name cannot contain double quotes, braces, or line breaks."
		s.render(w, "user-page", view)
		return
	}
	prev := s.cfg.User.Name
	s.cfg.User.Name = name
	if err := s.saveConfig(); err != nil {
		s.cfg.User.Name = prev
		view.Error = "Could not save: " + err.Error()
	} else {
		view.Message = "Saved."
	}
	s.render(w, "user-page", view)
}
