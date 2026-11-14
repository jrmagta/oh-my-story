package mail_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jrmagta/oh-my-story/internal/mail"
)

// A relay that accepts the connection and then says nothing must not hang the caller.
func TestSMTPStalledRelayTimesOut(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close() // hold the connection open, never send a greeting
		}
	}()

	s := mail.SMTP{Addr: ln.Addr().String(), From: "a@example.com", Timeout: 300 * time.Millisecond}
	done := make(chan error, 1)
	go func() {
		done <- s.Send(context.Background(), mail.Message{To: "b@example.com", Subject: "s", Body: "b"})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("send to a silent relay succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("send still blocked after 5s; the timeout did not apply")
	}
}

// TestSMTPToMailpit needs the compose Mailpit (make up). make test sets the variables.
func TestSMTPToMailpit(t *testing.T) {
	addr, api := os.Getenv("TEST_SMTP_ADDR"), os.Getenv("TEST_MAILPIT_URL")
	if addr == "" || api == "" {
		t.Skip("TEST_SMTP_ADDR / TEST_MAILPIT_URL not set; run make up && make test")
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	subject := "Résumé review " + id // non-ASCII must survive header encoding

	s := mail.SMTP{Addr: addr, From: "oh-my-story <no-reply@localhost>"}
	if err := s.Send(context.Background(), mail.Message{To: "Ann Example <ann@example.com>", Subject: subject, Body: "line one\nline two"}); err != nil {
		t.Fatal(err)
	}

	res, err := http.Get(api + "/api/v1/search?query=" + url.QueryEscape(id))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct {
		Messages []struct {
			Subject string
			To      []struct{ Address string }
		}
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Messages) != 1 {
		t.Fatalf("found %d messages for %s, want 1", len(out.Messages), id)
	}
	if m := out.Messages[0]; m.Subject != subject || len(m.To) != 1 || m.To[0].Address != "ann@example.com" {
		t.Errorf("delivered message: %+v", m)
	}
}
