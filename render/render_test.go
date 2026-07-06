package render

import (
	"runtime"
	"testing"
)

func TestBuildPlan_NoProfiles(t *testing.T) {
	p, err := BuildPlan("/proj", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "render.ps1" {
		t.Errorf("Name = %q, want render.ps1", p.Name)
	}
	// Either direct .ps1 execution (Windows + pwsh) or pwsh -File.
	if p.Cmd != "pwsh" && p.Cmd != "/proj/render.ps1" &&
		p.Cmd != `\proj\render.ps1` {
		// Accept forward or backward slashes per OS.
		if runtime.GOOS == "windows" {
			// nothing extra to check
		} else if p.Cmd != "pwsh" {
			t.Errorf("Cmd = %q, want pwsh (non-Windows)", p.Cmd)
		}
	}
}

func TestBuildPlan_OneProfileErrors(t *testing.T) {
	if _, err := BuildPlan("/proj", "only"); err == nil {
		t.Fatal("expected error for single profile, got nil")
	}
}

func TestBuildPlan_TwoProfilesQuarto(t *testing.T) {
	p, err := BuildPlan("/proj", "a,b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "quarto" || p.Cmd != "quarto" {
		t.Fatalf("plan = %+v, want quarto invocation", p)
	}
	want := []string{"render", "--profile", "a,b", "--no-clean"}
	if len(p.Args) != len(want) {
		t.Fatalf("args = %v, want %v", p.Args, want)
	}
	for i, a := range want {
		if p.Args[i] != a {
			t.Errorf("args[%d] = %q, want %q", i, p.Args[i], a)
		}
	}
}

func TestBuildPlan_SlidesFirst(t *testing.T) {
	p, err := BuildPlan("/proj", "slides,calltaker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "make_slides" {
		t.Fatalf("Name = %q, want make_slides", p.Name)
	}
	if p.Args[len(p.Args)-1] != "calltaker" {
		t.Errorf("last arg = %q, want calltaker", p.Args[len(p.Args)-1])
	}
}

func TestBuildPlan_SlidesSecondCaseInsensitive(t *testing.T) {
	p, err := BuildPlan("/proj", "calltaker,SLIDES")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "make_slides" {
		t.Fatalf("Name = %q, want make_slides", p.Name)
	}
	if p.Args[len(p.Args)-1] != "calltaker" {
		t.Errorf("last arg = %q, want calltaker", p.Args[len(p.Args)-1])
	}
}

func TestBuildPlan_BothSlidesFallsThroughToQuarto(t *testing.T) {
	p, err := BuildPlan("/proj", "slides,slides")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "quarto" {
		t.Errorf("Name = %q, want quarto (both slides -> no special-case)", p.Name)
	}
}

func TestBuildPlan_TooMany(t *testing.T) {
	if _, err := BuildPlan("/proj", "a,b,c"); err == nil {
		t.Fatal("expected error for 3 profiles, got nil")
	}
}

func TestSplitProfiles_TrimsAndDropsEmpties(t *testing.T) {
	got := splitProfiles(" a , , b ,")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("splitProfiles = %v, want [a b]", got)
	}
}
