package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"unicode/utf8"

	"github.com/gorilla/websocket"
	"github.com/klauspost/compress/zstd"
)

const daemonCompressionHeader = "x-codex-app-server-compression"
const daemonCompressionProtocol = "zstd-stream-v1"
const daemonMessageLimit = 16 << 20
const daemonZstdWindow = 1 << 20

// This is Codex's application-level, flushed zstd stream protocol, not the
// WebSocket permessage-deflate extension. Streams belong to one connection.
type daemonWire struct {
	conn    *websocket.Conn
	mu      sync.Mutex
	encoder *zstd.Encoder
	encoded bytes.Buffer
	decoder *daemonStreamDecoder
}

func newDaemonWire(conn *websocket.Conn, headers http.Header) (*daemonWire, error) {
	w := &daemonWire{conn: conn}
	conn.SetReadLimit(daemonMessageLimit + (1 << 20))
	values := headers.Values(daemonCompressionHeader)
	if len(values) == 0 {
		return w, nil
	}
	if len(values) != 1 || values[0] != daemonCompressionProtocol {
		return nil, errors.New("unsupported daemon WebSocket compression acknowledgement")
	}
	encoder, err := zstd.NewWriter(&w.encoded, zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(daemonZstdWindow), zstd.WithEncoderLevel(zstd.SpeedFastest))
	if err != nil {
		return nil, err
	}
	w.encoder = encoder
	w.decoder = newDaemonStreamDecoder()
	return w, nil
}

func (w *daemonWire) writeJSON(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > daemonMessageLimit {
		return errors.New("daemon message exceeds size limit")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.encoder == nil {
		return w.conn.WriteMessage(websocket.TextMessage, data)
	}
	w.encoded.Reset()
	if _, err = w.encoder.Write(data); err != nil {
		return err
	}
	if err = w.encoder.Flush(); err != nil {
		return err
	}
	return w.conn.WriteMessage(websocket.BinaryMessage, w.encoded.Bytes())
}

func (w *daemonWire) readJSON(value any) error {
	kind, data, err := w.conn.ReadMessage()
	if err != nil {
		return err
	}
	if w.decoder != nil {
		if kind != websocket.BinaryMessage {
			return errors.New("compressed daemon sent a non-binary message")
		}
		data, err = w.decoder.decode(data)
		if err != nil {
			return err
		}
	} else if kind != websocket.TextMessage {
		return errors.New("unnegotiated binary daemon message")
	}
	if len(data) > daemonMessageLimit || !utf8.Valid(data) {
		return errors.New("invalid or oversized daemon JSON message")
	}
	return json.Unmarshal(data, value)
}

func (w *daemonWire) close() {
	// Closing the socket interrupts its reader/writer before waiting for codec IO.
	_ = w.conn.Close()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.encoder != nil {
		_ = w.encoder.Close()
	}
	if w.decoder != nil {
		w.decoder.close()
	}
}

type daemonDecodedChunk struct {
	data []byte
	err  error
}

// The synchronous decoder writes each decoded block before requesting the
// next compressed block. At a validated flush boundary the reader hands back
// the collected message, then waits for the next WebSocket message without
// returning EOF (which would discard stream history). One worker per connection
// owns all decoder state and exits when the connection closes.
type daemonStreamDecoder struct {
	chunks   chan []byte
	results  chan daemonDecodedChunk
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
	inFrame  bool
	checksum bool
}

type daemonChunkReader struct {
	stream *daemonStreamDecoder
	input  []byte
	active bool
	output bytes.Buffer
}

func newDaemonStreamDecoder() *daemonStreamDecoder {
	d := &daemonStreamDecoder{chunks: make(chan []byte), results: make(chan daemonDecodedChunk), stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(d.done)
		reader := &daemonChunkReader{stream: d}
		decoder, err := zstd.NewReader(reader, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxWindow(daemonZstdWindow), zstd.WithDecoderMaxMemory(8<<20))
		if err == nil {
			_, err = decoder.WriteTo(reader)
			decoder.Close()
		}
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		select {
		case d.results <- daemonDecodedChunk{err: err}:
		case <-d.stop:
		}
	}()
	return d
}

func (r *daemonChunkReader) Read(p []byte) (int, error) {
	if len(r.input) == 0 {
		if r.active {
			result := daemonDecodedChunk{data: bytes.Clone(r.output.Bytes())}
			r.output.Reset()
			r.active = false
			select {
			case r.stream.results <- result:
			case <-r.stream.stop:
				return 0, io.EOF
			}
		}
		select {
		case r.input = <-r.stream.chunks:
			r.active = true
		case <-r.stream.stop:
			return 0, io.EOF
		}
	}
	n := copy(p, r.input)
	r.input = r.input[n:]
	return n, nil
}

func (r *daemonChunkReader) Write(p []byte) (int, error) {
	if len(p) > daemonMessageLimit-r.output.Len() {
		return 0, errors.New("decompressed daemon message exceeds size limit")
	}
	return r.output.Write(p)
}

func (d *daemonStreamDecoder) decode(data []byte) ([]byte, error) {
	if err := d.validateFlush(data); err != nil {
		return nil, err
	}
	select {
	case d.chunks <- data:
	case <-d.done:
		return nil, errors.New("daemon decompressor closed")
	case <-d.stop:
		return nil, io.EOF
	}
	select {
	case result := <-d.results:
		return result.data, result.err
	case <-d.done:
		return nil, errors.New("daemon decompressor failed")
	case <-d.stop:
		return nil, io.EOF
	}
}

func (d *daemonStreamDecoder) close() { d.once.Do(func() { close(d.stop) }); <-d.done }

// A partial block must never be joined to the next RPC message. Validate block
// framing first; the library then validates actual compressed data and checksums.
func (d *daemonStreamDecoder) validateFlush(data []byte) error {
	if len(data) == 0 || len(data) > daemonMessageLimit+(1<<20) {
		return errors.New("invalid compressed daemon message size")
	}
	inFrame, checksum := d.inFrame, d.checksum
	for len(data) > 0 {
		if !inFrame {
			var header zstd.Header
			if err := header.Decode(data); err != nil {
				return fmt.Errorf("daemon zstd header: %w", err)
			}
			if header.Skippable || header.DictionaryID != 0 || header.WindowSize > daemonZstdWindow || header.SingleSegment && header.FrameContentSize > daemonZstdWindow {
				return errors.New("unsupported daemon zstd frame")
			}
			data = data[header.HeaderSize:]
			inFrame = true
			checksum = header.HasCheckSum
		}
		if len(data) < 3 {
			return errors.New("truncated daemon zstd block header")
		}
		block := uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16
		size := int(block >> 3)
		kind := (block >> 1) & 3
		last := block&1 != 0
		if kind == 3 || size > 128<<10 {
			return errors.New("invalid daemon zstd block")
		}
		if kind == 1 {
			size = 1
		}
		if len(data) < 3+size {
			return errors.New("truncated daemon zstd block")
		}
		data = data[3+size:]
		if last {
			if checksum {
				if len(data) < 4 {
					return errors.New("truncated daemon zstd checksum")
				}
				data = data[4:]
			}
			inFrame = false
		}
	}
	d.inFrame, d.checksum = inFrame, checksum
	return nil
}
