// Package webhook sends queued webhook deliveries: signed JSON POSTs, retried
// on a schedule, from any number of server instances without doubling.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"github.com/jrmagta/oh-my-story/internal/store"
)

// Backoff is how long to wait before each retry after a failed send; after
// the last, the delivery is marked failed (six tries in all, over about 15 hours).
var Backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// Sign returns the X-OMS-Signature value for a body: sha256= and the hex HMAC.
func Sign(secret string, body []byte) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// metadata is the cloud metadata address over IPv6 (AWS); IPv4's
// 169.254.169.254 is covered by the link-local range.
var metadata = netip.MustParseAddr("fd00:ec2::254")

// ErrBlockedAddress is returned for a webhook address the server will not post to.
var ErrBlockedAddress = errors.New("webhooks cannot post to this address")

// Blocked says whether ip may not be posted to: link-local and cloud metadata
// addresses always; loopback and private ranges too when blockPrivate is set.
func Blocked(ip netip.Addr, blockPrivate bool) bool {
	ip = ip.Unmap()
	switch {
	case !ip.IsValid(), ip.IsUnspecified(), ip.IsMulticast(), ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast(), ip == metadata:
		return true
	case blockPrivate && (ip.IsLoopback() || ip.IsPrivate()):
		return true
	}
	return false
}

// CheckURL validates a webhook address when it is saved: http or https with
// a host, and not a blocked address when given as an IP. Names are checked
// again when connecting, against what they resolve to then.
func CheckURL(raw string, blockPrivate bool) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return errors.New("url must be an http or https address")
	}
	if len(raw) > 2000 {
		return errors.New("url must be at most 2,000 characters")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && Blocked(ip, blockPrivate) {
		return ErrBlockedAddress
	}
	return nil
}

// Sender posts due deliveries. Run one per server instance.
type Sender struct {
	store   *store.Store
	client  *http.Client
	backoff []time.Duration
	wake    chan struct{}
	// Lease is how long a claimed delivery is held before another sender may take it.
	Lease time.Duration
}

// New returns a Sender. blockPrivate also refuses loopback and private addresses.
func New(s *store.Store, blockPrivate bool) *Sender {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		// Check the address actually connected to, after DNS, so a name cannot point somewhere blocked.
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || Blocked(ip, blockPrivate) {
				return ErrBlockedAddress
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                 nil, // a proxy would connect for us and skip the address check
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		MaxIdleConnsPerHost:   2,
	}
	return &Sender{
		store: s,
		client: &http.Client{
			Transport: transport,
			Timeout:   15 * time.Second,
			// A redirect is an answer, not a place to follow; 3xx counts as a failure.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		backoff: Backoff,
		wake:    make(chan struct{}, 1),
		Lease:   2 * time.Minute,
	}
}

// WithBackoff replaces the retry schedule (tests use short ones).
func (s *Sender) WithBackoff(b []time.Duration) *Sender {
	s.backoff = b
	return s
}

// Wake asks a running sender to look for due deliveries now.
func (s *Sender) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run sends due deliveries until ctx ends: every two seconds, or when woken.
func (s *Sender) Run(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	for {
		for {
			n, err := s.RunOnce(ctx)
			if err != nil && ctx.Err() == nil {
				slog.Error("webhook deliveries", "err", err)
			}
			if n == 0 || err != nil {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-s.wake:
		case <-prune.C:
			if err := s.store.PruneDeliveries(ctx, 30*24*time.Hour); err != nil && ctx.Err() == nil {
				slog.Error("prune webhook deliveries", "err", err)
			}
		}
	}
}

// RunOnce claims a batch of due deliveries, sends them and records each
// result. It returns how many it sent or tried.
func (s *Sender) RunOnce(ctx context.Context) (int, error) {
	batch, err := s.store.ClaimDeliveries(ctx, 20, s.Lease)
	if err != nil {
		return 0, err
	}
	for _, d := range batch {
		code, sendErr := s.send(ctx, d)
		ok := sendErr == nil && code >= 200 && code < 300
		var errText *string
		switch {
		case sendErr != nil:
			t := sendErr.Error()
			errText = &t
		case !ok:
			t := "HTTP " + strconv.Itoa(code)
			errText = &t
		}
		var codePtr *int
		if code != 0 {
			codePtr = &code
		}
		var retry time.Duration
		if !ok && d.Attempts < len(s.backoff) {
			retry = s.backoff[d.Attempts]
			if retry == 0 {
				retry = time.Millisecond // due again at once (tests)
			}
		}
		if err := s.store.RecordDelivery(ctx, d.ID, ok, codePtr, errText, retry); err != nil {
			return len(batch), err
		}
	}
	return len(batch), nil
}

func (s *Sender) send(ctx context.Context, d store.OutgoingDelivery) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.URL, bytes.NewReader(d.Body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "oh-my-story-webhooks")
	req.Header.Set("X-OMS-Event", d.Type)
	req.Header.Set("X-OMS-Delivery", strconv.FormatInt(d.ID, 10))
	req.Header.Set("X-OMS-Signature", Sign(d.Secret, d.Body))
	res, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			return 0, ErrBlockedAddress
		}
		return 0, fmt.Errorf("post failed: %w", errors.Unwrap(err))
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	return res.StatusCode, nil
}
