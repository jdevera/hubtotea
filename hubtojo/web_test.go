package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestStatsPageRenders(t *testing.T) {
	var output bytes.Buffer
	store := NewStatsStore("test", 3600)

	err := statsPage.ExecuteTemplate(&output, "stats.html", store.Snapshot())
	if err != nil {
		t.Fatalf("render stats page: %v", err)
	}
	if !strings.Contains(output.String(), "HubToJo") {
		t.Fatal("rendered stats page does not contain the application name")
	}
}

func TestStatsPageRendersStarredRepositoryState(t *testing.T) {
	var output bytes.Buffer
	store := NewStatsStore("test", 3600)
	startedAt := time.Now().Add(-time.Second)
	store.FinishRun(RunStats{
		Status:               "success",
		StartedAt:            startedAt,
		StarredEnabled:       true,
		OwnedDiscovered:      3,
		StarredDiscovered:    12,
		Duplicates:           2,
		StarredBacklog:       4,
		Deferred:             4,
		DeferredRepositories: []string{"other/project"},
		StarredOrganization: &OrganizationStats{
			Name:   "github-stars",
			Result: OrganizationExisting,
		},
	}, time.Now())

	if err := statsPage.ExecuteTemplate(&output, "stats.html", store.Snapshot()); err != nil {
		t.Fatalf("render stats page: %v", err)
	}
	for _, content := range []string{"Starred discovered", "github-stars", "Starred backlog", "other/project"} {
		if !strings.Contains(output.String(), content) {
			t.Fatalf("rendered stats page does not contain %q", content)
		}
	}
}

func TestStartWebServerReturnsBindError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve test address: %v", err)
	}
	defer listener.Close()

	server, err := StartWebServer(
		listener.Addr().String(),
		NewStatsStore("test", 3600),
		NewMetrics("test", 3600),
	)
	if err == nil {
		server.Close()
		t.Fatal("expected an error when the web address is already in use")
	}
}

func TestStartWebServerServesEmbeddedIcon(t *testing.T) {
	server, err := StartWebServer(
		"127.0.0.1:0",
		NewStatsStore("test", 3600),
		NewMetrics("test", 3600),
	)
	if err != nil {
		t.Fatalf("start web server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Errorf("shut down web server: %v", err)
		}
	})

	response, err := http.Get("http://" + server.Addr + "/static/favicon.png")
	if err != nil {
		t.Fatalf("get favicon: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("favicon status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "image/png" {
		t.Fatalf("favicon content type = %q, want image/png", contentType)
	}

	signature := make([]byte, 8)
	if _, err := io.ReadFull(response.Body, signature); err != nil {
		t.Fatalf("read favicon signature: %v", err)
	}
	if !bytes.Equal(signature, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("favicon does not have a PNG signature: %x", signature)
	}
}
