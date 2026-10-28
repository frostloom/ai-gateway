package jev

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"syscall"
)

// ClassifyTransportFailureCause exposes the same finite typed classification
// to guarded evaluation transports. It never returns raw error text.
func ClassifyTransportFailureCause(err error) string { return transportFailureCause(err) }

// Classify typed errors only. Never inspect or retain raw text, addresses,
// proxy credentials or certificate subjects. A cause does not establish whether
// an inference reached the provider, was processed, or incurred a charge.
func transportFailureCause(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return "timeout"
	}
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var certificate x509.CertificateInvalidError
	if errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &certificate) {
		return "tls_certificate"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	for _, cause := range []struct {
		err  error
		name string
	}{
		{syscall.ECONNREFUSED, "connection_refused"},
		{syscall.ECONNRESET, "connection_reset"},
		{syscall.EPIPE, "broken_pipe"},
		{io.ErrUnexpectedEOF, "unexpected_eof"},
		{io.EOF, "eof"},
	} {
		if errors.Is(err, cause.err) {
			return cause.name
		}
	}
	return "unknown"
}
