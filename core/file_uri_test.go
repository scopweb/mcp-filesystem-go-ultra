package core

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileURIToPath_DriveLetter(t *testing.T) {
	got, err := FileURIToPath("file:///C:/proj")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		if !strings.EqualFold(got, `C:\proj`) {
			t.Fatalf("got %q want C:\\proj", got)
		}
	} else if !strings.HasPrefix(strings.ToUpper(got), "C:") {
		t.Fatalf("got %q", got)
	}
}

func TestFileURIToPath_LowercaseDrive(t *testing.T) {
	got, err := FileURIToPath("file:///c:/Users/foo")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		want := filepath.Clean(`c:\Users\foo`)
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	}
}

func TestFileURIToPath_Localhost(t *testing.T) {
	got, err := FileURIToPath("file://localhost/C:/x")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && !strings.EqualFold(got, `C:\x`) {
		t.Fatalf("got %q", got)
	}
}

func TestFileURIToPath_Unix(t *testing.T) {
	got, err := FileURIToPath("file:///home/user/proj")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.FromSlash("/home/user/proj"))
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFileURIToPath_PercentEncoding(t *testing.T) {
	got, err := FileURIToPath("file:///C:/My%20Proj")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "My Proj") {
		t.Fatalf("got %q", got)
	}
}

func TestFileURIToPath_WSLHost(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("UNC form is Windows-specific")
	}
	got, err := FileURIToPath("file://wsl.localhost/Ubuntu/home/u")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(got), `wsl.localhost`) {
		t.Fatalf("got %q", got)
	}
}

func TestFileURIToPath_RejectsNonFile(t *testing.T) {
	if _, err := FileURIToPath("https://example.com/x"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := FileURIToPath(""); err == nil {
		t.Fatal("expected error")
	}
}
