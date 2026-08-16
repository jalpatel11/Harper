package executor

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func requireDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available, skipping DockerExecutor test")
	}
	// The CLI can be installed with no daemon running (e.g. Docker Desktop
	// not started) — "docker info" only succeeds if the daemon is actually
	// reachable, which is what these tests really need.
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not reachable, skipping DockerExecutor test")
	}
}

func TestDockerExecutor_Exec_RunsInsideContainer(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()

	d, err := NewDockerExecutor(ctx, DockerOptions{
		Image:   "alpine:3.20",
		WorkDir: t.TempDir(),
		Network: "none",
	})
	if err != nil {
		t.Fatalf("NewDockerExecutor: %v", err)
	}
	defer d.Close(ctx)

	res, err := d.Exec(ctx, "echo hello-from-container", 10*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.Stdout != "hello-from-container\n" {
		t.Fatalf("unexpected stdout: %q", res.Stdout)
	}
}

func TestDockerExecutor_Exec_RunsFromWorkspaceDirectory(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()

	d, err := NewDockerExecutor(ctx, DockerOptions{
		Image:   "alpine:3.20",
		WorkDir: t.TempDir(),
		Network: "none",
	})
	if err != nil {
		t.Fatalf("NewDockerExecutor: %v", err)
	}
	defer d.Close(ctx)

	// docker exec does not inherit the working directory set on the
	// container's initial process via `docker run -w` unless -w is also
	// passed to docker exec itself — this guards against silently executing
	// commands from the wrong directory and breaking every relative path.
	res, err := d.Exec(ctx, "pwd", 10*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.Stdout != "/workspace\n" {
		t.Fatalf("expected commands to run from /workspace, got pwd: %q", res.Stdout)
	}
}

func TestDockerExecutor_Exec_NetworkNoneBlocksInternet(t *testing.T) {
	requireDocker(t)
	ctx := context.Background()

	d, err := NewDockerExecutor(ctx, DockerOptions{
		Image:   "alpine:3.20",
		WorkDir: t.TempDir(),
		Network: "none",
	})
	if err != nil {
		t.Fatalf("NewDockerExecutor: %v", err)
	}
	defer d.Close(ctx)

	res, err := d.Exec(ctx, "wget -T 3 -q -O - https://example.com || echo BLOCKED", 15*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.Stdout != "BLOCKED\n" {
		t.Fatalf("expected network to be blocked, got stdout: %q", res.Stdout)
	}
}
