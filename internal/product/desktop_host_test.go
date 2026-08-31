package product

import "testing"

func TestDesktopHost(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{
			name: "no host signals",
			env:  map[string]string{},
			want: "",
		},
		{
			name: "VS Code chrome desktop",
			env:  map[string]string{"CHROME_DESKTOP": "code.desktop"},
			want: DesktopHostCode,
		},
		{
			name: "Cursor chrome desktop",
			env:  map[string]string{"CHROME_DESKTOP": "cursor.desktop"},
			want: DesktopHostCursor,
		},
		{
			name: "VS Code gio launched",
			env:  map[string]string{"GIO_LAUNCHED_DESKTOP_FILE": "/usr/share/applications/code.desktop"},
			want: DesktopHostCode,
		},
		{
			name: "VS Code askpass node",
			env:  map[string]string{"VSCODE_GIT_ASKPASS_NODE": "/usr/share/code/code"},
			want: DesktopHostCode,
		},
		{
			name: "VS Code askpass main",
			env:  map[string]string{"VSCODE_GIT_ASKPASS_MAIN": "/usr/share/code/resources/app/extensions/git/dist/askpass-main.js"},
			want: DesktopHostCode,
		},
		{
			name: "Cursor askpass node",
			env:  map[string]string{"VSCODE_GIT_ASKPASS_NODE": "/opt/cursor/resources/app/extensions/git/dist/askpass-main.js"},
			want: DesktopHostCursor,
		},
		{
			name: "case insensitive",
			env:  map[string]string{"CHROME_DESKTOP": "Cursor.desktop"},
			want: DesktopHostCursor,
		},
		{
			name: "cursor wins over code",
			env: map[string]string{
				"GIO_LAUNCHED_DESKTOP_FILE": "/usr/share/applications/code.desktop",
				"CHROME_DESKTOP":            "cursor.desktop",
			},
			want: DesktopHostCursor,
		},
		{
			name: "code-oss desktop file matches",
			env:  map[string]string{"CHROME_DESKTOP": "code-oss.desktop"},
			want: DesktopHostCode,
		},
		{
			name: "code-oss askpass path does not match",
			env:  map[string]string{"VSCODE_GIT_ASKPASS_NODE": "/usr/share/code-oss/code-oss"},
			want: "",
		},
		{
			name: "unrelated values",
			env: map[string]string{
				"CHROME_DESKTOP":            "firefox.desktop",
				"VSCODE_GIT_ASKPASS_NODE":   "/usr/bin/node",
				"GIO_LAUNCHED_DESKTOP_FILE": "/usr/share/applications/org.gnome.Terminal.desktop",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, name := range allHostVars {
				t.Setenv(name, "")
			}
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			if got := DesktopHost(); got != tt.want {
				t.Errorf("DesktopHost() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsVSCodeDesktopHost(t *testing.T) {
	t.Setenv("GIO_LAUNCHED_DESKTOP_FILE", "")
	t.Setenv("CHROME_DESKTOP", "code.desktop")
	t.Setenv("VSCODE_GIT_ASKPASS_NODE", "")
	t.Setenv("VSCODE_GIT_ASKPASS_MAIN", "")

	if !IsVSCodeDesktopHost() {
		t.Error("IsVSCodeDesktopHost() = false with CHROME_DESKTOP=code.desktop, want true")
	}
	if IsCursorDesktopHost() {
		t.Error("IsCursorDesktopHost() = true with CHROME_DESKTOP=code.desktop, want false")
	}
}
