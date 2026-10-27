//go:build unix

package codex

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Opt-in integration check: no credentials, user configuration, model requests,
// thread creation or shared daemon are used. CI otherwise uses wire fixtures.
func TestNativeCodex164Compression(t *testing.T) {
	if os.Getenv("CODEXOMETER_TEST_CODEX_164") != "1" {
		t.Skip("set CODEXOMETER_TEST_CODEX_164=1 with Codex 0.164.0 installed")
	}
	binary, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.Command(binary, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "codex-cli 0.164.0" {
		t.Fatal("expected Codex 0.164.0", string(version), err)
	}
	home, err := os.MkdirTemp("/tmp", "cxm-native-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, binary, "-c", "features.app_server_websocket_compression=true", "app-server", "--listen", "unix://")
	command.Dir = home
	command.Env = environmentWith(environmentWithout(os.Environ(), "CODEX_HOME", "OPENAI_API_KEY", "CODEX_API_KEY"), "CODEX_HOME", home)
	log, err := os.Create(filepath.Join(home, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = log
	command.Stdout = log
	if err := command.Start(); err != nil {
		cancel()
		log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); command.Wait(); log.Close() })
	p := newSessionStatusProvider(home).(*daemonStatusProvider)
	t.Cleanup(func() { p.disconnect(nil) })
	deadline := time.Now().Add(10 * time.Second)
	for {
		err = p.ensureConnected(context.Background())
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native handshake failed", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.mu.Lock()
	compressed := p.wire != nil && p.wire.decoder != nil
	p.mu.Unlock()
	if !compressed {
		t.Fatal("Codex did not acknowledge zstd-stream-v1")
	}
	for range 5 {
		var result struct {
			Data []struct {
				Name    string
				Enabled bool
			}
		}
		if err := p.request(context.Background(), "experimentalFeature/list", map[string]any{}, &result); err != nil {
			t.Fatal("native compressed RPC", err)
		}
		if len(result.Data) == 0 {
			t.Fatal("native feature inventory empty")
		}
	}
	loaded, err := p.loadedThreads(context.Background())
	if err != nil || len(loaded) != 0 {
		t.Fatal("isolated server unexpectedly has loaded threads", loaded, err)
	}
}
