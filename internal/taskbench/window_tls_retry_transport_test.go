package taskbench

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type windowTLSProtocolBody struct {
	reads  atomic.Int64
	reader *strings.Reader
}

func (b *windowTLSProtocolBody) Read(p []byte) (int, error) {
	b.reads.Add(1)
	return b.reader.Read(p)
}
func (*windowTLSProtocolBody) Close() error { return nil }

// Exercise the real standard-library callback sequence over memory pipes.
// Each path has exactly one RoundTrip; neither negotiates HTTP/2 nor proves
// general native-attempt counts, billing, retry permission or runtime readiness.
func TestWindowTLSRetryWitnessRealTransportEOF(t *testing.T) {
	for _, proxy := range []bool{false, true} {
		name := "direct_https"
		if proxy {
			name = "http_connect_tunnel"
		}
		t.Run(name, func(t *testing.T) {
			parent, cancelParent := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelParent()
			requestContext, cancelRequest := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelRequest()
			client, server := net.Pipe()
			defer client.Close()
			defer server.Close()
			deadline, _ := requestContext.Deadline()
			if err := client.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			if err := server.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			peerDone := make(chan error, 1)
			go func() {
				defer server.Close()
				reader := bufio.NewReader(server)
				if proxy {
					connect, err := http.ReadRequest(reader)
					if err != nil {
						peerDone <- fmt.Errorf("read CONNECT: %w", err)
						return
					}
					if connect.Method != http.MethodConnect || connect.Host != "tls-witness.invalid:443" || connect.URL.Host != "tls-witness.invalid:443" {
						peerDone <- fmt.Errorf("unexpected CONNECT method/host: %s %s %s", connect.Method, connect.Host, connect.URL.Host)
						return
					}
					if _, err = io.WriteString(server, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
						peerDone <- fmt.Errorf("CONNECT response: %w", err)
						return
					}
				}
				peerDone <- windowTLSProtocolReadClientHello(reader)
			}()
			var dials atomic.Int64
			transport := &http.Transport{
				ForceAttemptHTTP2: true,
				DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
					if dials.Add(1) != 1 {
						return nil, errors.New("unexpected additional dial")
					}
					want := "tls-witness.invalid:443"
					if proxy {
						want = "proxy-witness.invalid:8080"
					}
					if network != "tcp" || address != want {
						return nil, fmt.Errorf("unexpected dial %s %s", network, address)
					}
					return client, nil
				},
			}
			if proxy {
				transport.Proxy = http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy-witness.invalid:8080"})
			}
			defer transport.CloseIdleConnections()
			body := &windowTLSProtocolBody{reader: strings.NewReader("must-not-be-read")}
			ctx, witness := newWindowTLSRetryWitness(requestContext, parent)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://tls-witness.invalid/never-sent", body)
			if err != nil {
				t.Fatal(err)
			}
			req.ContentLength = int64(body.reader.Len())
			response, roundTripErr := transport.RoundTrip(req)
			witness.seal(response, roundTripErr)
			// Receive peer validation; successful RoundTrip EOF also requires the pipe to have closed.
			select {
			case peerErr := <-peerDone:
				if peerErr != nil {
					t.Errorf("peer: %v", peerErr)
				}
			case <-requestContext.Done():
				_ = client.Close()
				_ = server.Close()
				// Both endpoints are now closed: the peer's bounded read/write must exit.
				select {
				case peerErr := <-peerDone:
					t.Errorf("peer exceeded request deadline: %v", peerErr)
				case <-time.After(time.Second):
					t.Fatal("memory peer did not exit after pipe closure")
				}
			}
			snapshot := witness.snapshot()
			t.Logf("dials=%d body_reads=%d response_nil=%t eof=%t witness=%+v", dials.Load(), body.reads.Load(), response == nil, errors.Is(roundTripErr, io.EOF), snapshot)
			if dials.Load() != 1 || body.reads.Load() != 0 {
				t.Errorf("dials/body reads = %d/%d, want 1/0", dials.Load(), body.reads.Load())
			}
			if response != nil {
				_ = response.Body.Close()
				t.Errorf("unexpected response: %d", response.StatusCode)
			}
			if !errors.Is(roundTripErr, io.EOF) {
				t.Errorf("RoundTrip error = %v, want EOF", roundTripErr)
			}
			if parent.Err() != nil || requestContext.Err() != nil {
				t.Errorf("original contexts no longer live: parent=%v request=%v", parent.Err(), requestContext.Err())
			}
			if snapshot.TLSStarts != 1 || snapshot.TLSDone != 1 || snapshot.TLSFailedEOF != 1 || !witness.eligible() {
				t.Errorf("real handshake EOF rejected: %+v", snapshot)
			}
		})
	}
}

func windowTLSProtocolReadClientHello(reader io.Reader) error {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return fmt.Errorf("read TLS record header: %w", err)
	}
	length := int(binary.BigEndian.Uint16(header[3:]))
	if header[0] != 22 || header[1] != 3 || length < 4 || length > 16384 {
		return fmt.Errorf("unexpected ClientHello record header: %v", header)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return fmt.Errorf("read complete ClientHello record: %w", err)
	}
	helloLength := int(payload[1])<<16 | int(payload[2])<<8 | int(payload[3])
	if payload[0] != 1 || helloLength != length-4 {
		return fmt.Errorf("incomplete or non-ClientHello handshake: type=%d hello_length=%d record_length=%d", payload[0], helloLength, length)
	}
	return nil
}
