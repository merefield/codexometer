package codex

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
)

func TestDaemonDecoderNativeZstdFlushes(t *testing.T) {
	// Generated with native libzstd ZSTD_compressStream2(ZSTD_e_flush), the same
	// stream framing used by Codex's Rust zstd crate. Later messages reuse history.
	var fixtures []struct{ JSON, Hex string }
	data, err := os.ReadFile("testdata/zstd-stream-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	d := newDaemonStreamDecoder()
	defer d.close()
	for _, fixture := range fixtures {
		input, err := hex.DecodeString(fixture.Hex)
		if err != nil {
			t.Fatal(err)
		}
		got, err := d.decode(input)
		if err != nil || string(got) != fixture.JSON {
			t.Fatal("native stream failed", string(got), err)
		}
	}
	// Reconnect must discard the previous connection's dictionary/history.
	fresh := newDaemonStreamDecoder()
	defer fresh.close()
	second, _ := hex.DecodeString(fixtures[1].Hex)
	if _, err := fresh.decode(second); err == nil {
		t.Fatal("continuation accepted without first frame")
	}
}

func TestDaemonWireNegotiationAndStreaming(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "zstd"}[compressed], func(t *testing.T) {
			serverErr := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				header := http.Header{}
				if compressed {
					header.Set(daemonCompressionHeader, daemonCompressionProtocol)
				}
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, header)
				if err != nil {
					serverErr <- err
					return
				}
				wire, err := newDaemonWire(conn, header)
				if err != nil {
					serverErr <- err
					return
				}
				defer wire.close()
				for i := 0; i < 8; i++ {
					var request map[string]any
					if err = wire.readJSON(&request); err != nil {
						serverErr <- err
						return
					}
					if err = wire.writeJSON(request); err != nil {
						serverErr <- err
						return
					}
				}
				serverErr <- nil
			}))
			defer server.Close()
			conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), http.Header{http.CanonicalHeaderKey(daemonCompressionHeader): []string{daemonCompressionProtocol}})
			if err != nil {
				t.Fatal(err)
			}
			wire, err := newDaemonWire(conn, response.Header)
			if err != nil {
				t.Fatal(err)
			}
			defer wire.close()
			for i := 0; i < 8; i++ {
				text := strings.Repeat("retained stream history ", 15000)
				if err = wire.writeJSON(map[string]any{"id": i, "text": text}); err != nil {
					t.Fatal(err)
				}
				var result struct {
					ID   int
					Text string
				}
				if err = wire.readJSON(&result); err != nil || result.ID != i || result.Text != text {
					t.Fatal("round trip", result.ID, err)
				}
			}
			if err := <-serverErr; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDaemonDecoderRejectsBrokenAndOversizedFrames(t *testing.T) {
	var encoded bytes.Buffer
	encoder, err := zstd.NewWriter(&encoded, zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(daemonZstdWindow))
	if err != nil {
		t.Fatal(err)
	}
	encoder.Write([]byte(`{"result":"ok"}`))
	encoder.Flush()
	valid := bytes.Clone(encoded.Bytes())
	encoder.Close()
	for _, bad := range [][]byte{nil, []byte("invalid zstd"), valid[:len(valid)-1], append(bytes.Clone(valid), 1)} {
		d := newDaemonStreamDecoder()
		if _, err := d.decode(bad); err == nil {
			t.Error("broken frame accepted")
		}
		d.close()
	}
	encoded.Reset()
	encoder, _ = zstd.NewWriter(&encoded, zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(daemonZstdWindow))
	encoder.Write(bytes.Repeat([]byte{'x'}, daemonMessageLimit+1))
	encoder.Flush()
	d := newDaemonStreamDecoder()
	if _, err := d.decode(encoded.Bytes()); err == nil {
		t.Fatal("decompression limit ignored")
	}
	d.close()
	encoder.Close()
}

func TestDaemonDecoderCloseUnblocksIdleStream(t *testing.T) {
	d := newDaemonStreamDecoder()
	done := make(chan struct{})
	go func() { d.close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle decoder leaked")
	}
	d.close()
}

func TestDaemonWireRejectsUnknownAcknowledgement(t *testing.T) {
	conn := &websocket.Conn{}
	for _, values := range [][]string{{"future"}, {daemonCompressionProtocol, daemonCompressionProtocol}} {
		if _, err := newDaemonWire(conn, http.Header{http.CanonicalHeaderKey(daemonCompressionHeader): values}); err == nil {
			t.Fatal("unknown negotiation accepted")
		}
	}
}

func TestDaemonWireRejectsWrongMessageType(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			headers := http.Header{}
			if compressed {
				headers.Set(daemonCompressionHeader, daemonCompressionProtocol)
			}
			conn, err := (&websocket.Upgrader{}).Upgrade(w, r, headers)
			if err != nil {
				return
			}
			defer conn.Close()
			kind := websocket.BinaryMessage
			if compressed {
				kind = websocket.TextMessage
			}
			_ = conn.WriteMessage(kind, []byte(`{"result":{}}`))
		}))
		conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := newDaemonWire(conn, response.Header)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := wire.readJSON(&value); err == nil {
			t.Error("message type accepted without matching negotiation")
		}
		wire.close()
		server.Close()
	}
}
