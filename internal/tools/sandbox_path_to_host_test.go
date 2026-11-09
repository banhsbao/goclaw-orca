package tools

import "testing"

func TestSandboxPathToHost_AbsoluteHostPathNotDoubled(t *testing.T) {
	mount := "/app/workspace/teams/t1/u1"
	cwd := "/workspace"
	containerPath := ResolveSandboxPath(MapHostPathToSandbox(mount+"/specs/a.md", mount, cwd), cwd)
	if got := sandboxPathToHost(containerPath, mount, cwd); got != mount+"/specs/a.md" {
		t.Fatalf("got %q", got)
	}
}

func TestSandboxPathToHost_RelativeAndContainerPaths(t *testing.T) {
	mount := "/app/workspace/teams/t1/u1"
	cwd := "/workspace"
	cases := map[string]string{
		"content/x.json":            mount + "/content/x.json",
		"/workspace/content/x.json": mount + "/content/x.json",
	}
	for in, want := range cases {
		containerPath := ResolveSandboxPath(MapHostPathToSandbox(in, mount, cwd), cwd)
		if got := sandboxPathToHost(containerPath, mount, cwd); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
