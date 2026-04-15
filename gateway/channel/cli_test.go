package gatewaychannel_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	channel "github.com/mossagents/moss/gateway/channel"
	kchannel "github.com/mossagents/moss/kernel/channel"
)

func TestNewCLI_Defaults(t *testing.T) {
	c := channel.NewCLI()
	if c.Name() != "cli" {
		t.Errorf("expected name=cli, got %s", c.Name())
	}
}

func TestCLI_Close(t *testing.T) {
	c := channel.NewCLI()
	if err := c.Close(); err != nil {
		t.Errorf("unexpected error on Close: %v", err)
	}
}

func TestCLI_Send(t *testing.T) {
	var buf strings.Builder
	c := channel.NewCLI(channel.WithWriter(&buf))

	err := c.Send(context.Background(), kchannel.OutboundMessage{
		Content: "hello world",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "hello world") {
		t.Errorf("expected output to contain 'hello world', got: %q", buf.String())
	}
}

func TestCLI_Receive_SingleLine(t *testing.T) {
	input := "test input\n"
	c := channel.NewCLI(
		channel.WithReader(strings.NewReader(input)),
		channel.WithWriter(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := c.Receive(ctx)
	msg, ok := <-ch
	if !ok {
		t.Fatal("expected a message, channel was closed")
	}
	if msg.Content != "test input" {
		t.Errorf("expected content='test input', got %q", msg.Content)
	}
	if msg.ChannelName != "cli" {
		t.Errorf("expected channel name=cli, got %q", msg.ChannelName)
	}
	if msg.SenderID != "cli" {
		t.Errorf("expected sender_id=cli, got %q", msg.SenderID)
	}
	// After single-line EOF, channel should close
	_, ok = <-ch
	if ok {
		t.Error("expected channel to be closed after EOF")
	}
}

func TestCLI_Receive_MultipleLines(t *testing.T) {
	input := "first\nsecond\nthird\n"
	c := channel.NewCLI(
		channel.WithReader(strings.NewReader(input)),
		channel.WithWriter(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := c.Receive(ctx)
	var msgs []string
	for msg := range ch {
		msgs = append(msgs, msg.Content)
	}
	if len(msgs) != 3 {
		t.Errorf("expected 3 messages, got %d: %v", len(msgs), msgs)
	}
}

func TestCLI_Receive_SkipsEmptyLines(t *testing.T) {
	input := "\n\nhello\n\n"
	c := channel.NewCLI(
		channel.WithReader(strings.NewReader(input)),
		channel.WithWriter(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := c.Receive(ctx)
	var msgs []string
	for msg := range ch {
		msgs = append(msgs, msg.Content)
	}
	if len(msgs) != 1 || msgs[0] != "hello" {
		t.Errorf("expected 1 message 'hello', got: %v", msgs)
	}
}

func TestCLI_Receive_ExitCommand(t *testing.T) {
	input := "/exit\n"
	c := channel.NewCLI(
		channel.WithReader(strings.NewReader(input)),
		channel.WithWriter(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := c.Receive(ctx)
	msgs := collect(ch)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after /exit, got %d", len(msgs))
	}
}

func TestCLI_Receive_QuitCommand(t *testing.T) {
	input := "/quit\n"
	c := channel.NewCLI(
		channel.WithReader(strings.NewReader(input)),
		channel.WithWriter(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := c.Receive(ctx)
	msgs := collect(ch)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after /quit, got %d", len(msgs))
	}
}

func TestCLI_Receive_ExitCaseInsensitive(t *testing.T) {
	input := "/EXIT\n"
	c := channel.NewCLI(
		channel.WithReader(strings.NewReader(input)),
		channel.WithWriter(io.Discard),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := c.Receive(ctx)
	msgs := collect(ch)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages after /EXIT, got %d", len(msgs))
	}
}

func TestCLI_Receive_ContextCancellation(t *testing.T) {
	// Pipe that won't EOF - we cancel context instead
	pr, pw := io.Pipe()
	defer pw.Close()

	c := channel.NewCLI(
		channel.WithReader(pr),
		channel.WithWriter(io.Discard),
	)

	ctx, cancel := context.WithCancel(context.Background())
	ch := c.Receive(ctx)

	// Send one message then cancel context
	go func() {
		pw.Write([]byte("msg\n"))
		time.Sleep(50 * time.Millisecond)
		cancel()
		pw.Close()
	}()

	// Drain channel
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // channel closed properly
			}
		case <-timeout:
			t.Error("channel did not close after context cancellation")
			return
		}
	}
}

func TestCLI_WithCustomPrompt(t *testing.T) {
	var buf strings.Builder
	c := channel.NewCLI(
		channel.WithPrompt("$ "),
		channel.WithReader(strings.NewReader("hello\n")),
		channel.WithWriter(&buf),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := c.Receive(ctx)
	collect(ch)

	if !strings.Contains(buf.String(), "$ ") {
		t.Errorf("expected prompt '$ ' in output, got: %q", buf.String())
	}
}

func collect(ch <-chan kchannel.InboundMessage) []kchannel.InboundMessage {
	var msgs []kchannel.InboundMessage
	for msg := range ch {
		msgs = append(msgs, msg)
	}
	return msgs
}
