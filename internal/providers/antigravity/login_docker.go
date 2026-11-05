package antigravity

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/google/uuid"
)

const killStaleScript = `for d in /proc/[0-9]*; do
p=${d#/proc/}
[ "$p" = "$$" ] && continue
c=$(tr '\0' ' ' < "$d/cmdline" 2>/dev/null) || continue
case "$c" in
scrip[t]\ -qfc*) kill -9 "$p" 2>/dev/null;;
esac
[ "$c" = "$1 " ] && kill -9 "$p" 2>/dev/null
done
exit 0`

const killPidScript = `f=$1
pid=$(cat "$f" 2>/dev/null)
rm -f "$f"
case "$pid" in ''|*[!0-9]*) exit 0;; esac
for d in /proc/[0-9]*; do
p=${d#/proc/}
s=$(cat "$d/stat" 2>/dev/null) || continue
s=${s##*) }
set -- $s
[ "$2" = "$pid" ] && kill -9 "$p" 2>/dev/null
done
kill -9 "$pid" 2>/dev/null
exit 0`

const launchScript = `echo $$ > "$1"; exec script -qfc "$2" /dev/null`

func execInContainer(id uuid.UUID, args ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = docker(ctx, append([]string{"exec", ContainerName(id)}, args...)...)
}

func killStale(id uuid.UUID) {
	execInContainer(id, "sh", "-c", killStaleScript, "sh", agyPath())
}

func killByPidFile(id uuid.UUID, pidFile string) {
	execInContainer(id, "sh", "-c", killPidScript, "sh", pidFile)
}

func dockerStarter(id uuid.UUID, out io.Writer) (*Proc, error) {
	killStale(id)
	procCtx, cancel := context.WithCancel(context.Background())
	pidFile := "/tmp/agy-login-" + uuid.NewString() + ".pid"
	shellCmd := "stty cols 2000 rows 50; exec " + agyPath()
	cmd := exec.CommandContext(procCtx, "docker", "exec", "-i", ContainerName(id), "sh", "-c", launchScript, "sh", pidFile, shellCmd)
	cmd.Stdout = out
	cmd.Stderr = out
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start login: %w", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	return &Proc{
		Stdin: stdin,
		Done:  done,
		Kill: func() {
			cancel()
			killByPidFile(id, pidFile)
		},
	}, nil
}
