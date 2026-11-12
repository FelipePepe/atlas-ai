package platform

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		want    Paths
		wantErr string
	}{
		{
			name: "linux default state dir",
			goos: "linux",
			env:  map[string]string{"HOME": "/home/ana"},
			want: Paths{Home: "/home/ana", StateDir: "/home/ana/.local/state/atlas-ai"},
		},
		{
			name: "linux with absolute XDG_STATE_HOME",
			goos: "linux",
			env:  map[string]string{"HOME": "/home/ana", "XDG_STATE_HOME": "/var/xdg"},
			want: Paths{Home: "/home/ana", StateDir: "/var/xdg/atlas-ai"},
		},
		{
			name: "linux relative XDG_STATE_HOME is ignored",
			goos: "linux",
			env:  map[string]string{"HOME": "/home/ana", "XDG_STATE_HOME": "state"},
			want: Paths{Home: "/home/ana", StateDir: "/home/ana/.local/state/atlas-ai"},
		},
		{
			name: "linux trailing slash in HOME is cleaned",
			goos: "linux",
			env:  map[string]string{"HOME": "/home/ana/"},
			want: Paths{Home: "/home/ana", StateDir: "/home/ana/.local/state/atlas-ai"},
		},
		{
			name:    "linux empty HOME",
			goos:    "linux",
			env:     map[string]string{},
			wantErr: "HOME",
		},
		{
			name:    "linux relative HOME",
			goos:    "linux",
			env:     map[string]string{"HOME": "ana"},
			wantErr: "HOME",
		},
		{
			name: "windows happy path",
			goos: "windows",
			env: map[string]string{
				"USERPROFILE":  `C:\Users\Ana`,
				"LOCALAPPDATA": `C:\Users\Ana\AppData\Local`,
			},
			want: Paths{Home: `C:\Users\Ana`, StateDir: `C:\Users\Ana\AppData\Local\atlas-ai`},
		},
		{
			name: "windows trailing separator",
			goos: "windows",
			env: map[string]string{
				"USERPROFILE":  `C:\Users\Ana\`,
				"LOCALAPPDATA": `C:\Users\Ana\AppData\Local\`,
			},
			want: Paths{Home: `C:\Users\Ana`, StateDir: `C:\Users\Ana\AppData\Local\atlas-ai`},
		},
		{
			name: "windows UNC path",
			goos: "windows",
			env: map[string]string{
				"USERPROFILE":  `\\server\share\ana`,
				"LOCALAPPDATA": `\\server\share\ana\local`,
			},
			want: Paths{Home: `\\server\share\ana`, StateDir: `\\server\share\ana\local\atlas-ai`},
		},
		{
			name: "windows drive root has no doubled separator",
			goos: "windows",
			env:  map[string]string{"USERPROFILE": `C:\Users\Ana`, "LOCALAPPDATA": `D:\`},
			want: Paths{Home: `C:\Users\Ana`, StateDir: `D:\atlas-ai`},
		},
		{
			name:    "windows missing USERPROFILE",
			goos:    "windows",
			env:     map[string]string{"LOCALAPPDATA": `C:\Users\Ana\AppData\Local`},
			wantErr: "USERPROFILE",
		},
		{
			name:    "windows missing LOCALAPPDATA",
			goos:    "windows",
			env:     map[string]string{"USERPROFILE": `C:\Users\Ana`},
			wantErr: "LOCALAPPDATA",
		},
		{
			name:    "windows relative USERPROFILE",
			goos:    "windows",
			env:     map[string]string{"USERPROFILE": `Users\Ana`, "LOCALAPPDATA": `C:\x`},
			wantErr: "USERPROFILE",
		},
		{
			name:    "windows drive-relative LOCALAPPDATA",
			goos:    "windows",
			env:     map[string]string{"USERPROFILE": `C:\Users\Ana`, "LOCALAPPDATA": `C:x`},
			wantErr: "LOCALAPPDATA",
		},
		{
			name:    "unsupported platform",
			goos:    "plan9",
			env:     map[string]string{"HOME": "/home/ana"},
			wantErr: "unsupported platform",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.goos, env(tt.env))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got none (paths %+v)", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
