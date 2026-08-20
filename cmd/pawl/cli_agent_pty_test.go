//go:build unix

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func waitProcess(t *testing.T, cmd *exec.Cmd, timeout time.Duration) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("process did not exit within %s", timeout)
		return nil
	}
}

// This is the real process path behind the three-stream policy. A unit test of
// booleans alone would not catch runAgent forgetting to inspect stderr again.
func TestAgentWithHiddenStderrPrintsInsteadOfWaiting(t *testing.T) {
	dir := t.TempDir()
	master, terminal, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()

	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()

	cmd := exec.Command(pawlBin, "agent")
	cmd.Dir = dir
	cmd.Env = baseEnv()
	cmd.Stdin = terminal
	cmd.Stdout = terminal
	cmd.Stderr = null
	if err := cmd.Start(); err != nil {
		terminal.Close()
		t.Fatal(err)
	}
	terminal.Close()

	readDone := make(chan []byte, 1)
	go func() {
		out, _ := io.ReadAll(master) // PTYs commonly finish reads with EIO.
		readDone <- out
	}()
	if err := waitProcess(t, cmd, 5*time.Second); err != nil {
		t.Fatalf("agent exit: %v", err)
	}
	out := string(<-readDone)
	if strings.Contains(out, "Where should the pawl block go?") {
		t.Fatalf("hidden stderr session still prompted:\n%s", out)
	}
	if !strings.Contains(out, "pawl check --format json") {
		t.Fatalf("hidden stderr session did not print the block:\n%s", out)
	}
}

func TestAgentWithAllStreamsOnPTYPromptsAndAcceptsPrintChoice(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(pawlBin, "agent")
	cmd.Dir = dir
	cmd.Env = baseEnv()
	master, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()

	var out strings.Builder
	promptSeen := make(chan struct{}, 1)
	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		signaled := false
		for {
			n, err := master.Read(buf)
			if n > 0 {
				out.Write(buf[:n])
				if !signaled && strings.Contains(out.String(), "Choose [1-3]:") {
					signaled = true
					promptSeen <- struct{}{}
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					readDone <- nil
				} else {
					// Linux PTYs report EIO when the slave closes.
					readDone <- nil
				}
				return
			}
		}
	}()

	select {
	case <-promptSeen:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("interactive prompt was not visible")
	}
	if _, err := master.Write([]byte("3\n")); err != nil {
		t.Fatal(err)
	}
	if err := waitProcess(t, cmd, 5*time.Second); err != nil {
		t.Fatalf("agent exit: %v", err)
	}
	<-readDone
	text := out.String()
	if !strings.Contains(text, "Where should the pawl block go?") || !strings.Contains(text, "pawl check --format json") {
		t.Fatalf("interactive print path incomplete:\n%s", text)
	}
}
