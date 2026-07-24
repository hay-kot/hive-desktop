package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchManifest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.json" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("User-Agent") != "hive-desktop-release/1" {
			http.Error(w, "unexpected user agent", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"channel":"dev","version":"0.1.8-dev.1","pub_date":"2026-07-24T00:00:00Z","platforms":{"darwin-universal":{"url":"https://example.com/Hive.zip","sha256":"0000000000000000000000000000000000000000000000000000000000000000","size":1}}}`))
	}))
	defer server.Close()

	manifest, err := fetchManifest(context.Background(), server.Client(), server.URL+"/latest.json")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Channel != "dev" || manifest.Version != "0.1.8-dev.1" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}

	_, err = fetchManifest(context.Background(), server.Client(), server.URL+"/missing.json")
	if !errors.Is(err, errManifestNotFound) {
		t.Fatalf("missing manifest error = %v", err)
	}
}

func TestValidateManifestAdvancementChecksCascade(t *testing.T) {
	t.Parallel()

	candidate := mustParseVersion(t, "1.2.3")
	manifests := map[string]channelManifest{
		"stable": {Version: "1.2.2"},
		"beta":   {Version: "1.2.3-beta.1"},
		"dev":    {Version: "1.2.4-dev.1"},
	}
	if err := validateManifestAdvancement(candidate, manifests); err == nil {
		t.Fatal("validateManifestAdvancement unexpectedly allowed a dev channel downgrade")
	}

	candidate = mustParseVersion(t, "1.2.4")
	if err := validateManifestAdvancement(candidate, manifests); err != nil {
		t.Fatalf("validateManifestAdvancement rejected advancing stable release: %v", err)
	}
}

func TestFetchManifestRejectsMissingFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"channel":"dev"}`))
	}))
	defer server.Close()

	if _, err := fetchManifest(context.Background(), server.Client(), server.URL); err == nil {
		t.Fatal("fetchManifest unexpectedly accepted an incomplete manifest")
	}
}
