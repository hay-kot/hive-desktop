package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNextVersion(t *testing.T) {
	t.Parallel()

	dev1 := mustParseVersion(t, "0.1.8-dev.1")
	beta1 := mustParseVersion(t, "0.1.8-beta.1")
	stable := mustParseVersion(t, "0.1.8")

	tests := []struct {
		name     string
		channel  string
		versions []releaseVersion
		want     string
	}{
		{name: "first dev", channel: "dev", want: "0.1.0-dev.1"},
		{name: "increment dev", channel: "dev", versions: []releaseVersion{dev1}, want: "0.1.8-dev.2"},
		{name: "promote dev to beta", channel: "beta", versions: []releaseVersion{dev1}, want: "0.1.8-beta.1"},
		{name: "promote beta to stable", channel: "stable", versions: []releaseVersion{beta1}, want: "0.1.8"},
		{name: "dev after beta advances patch", channel: "dev", versions: []releaseVersion{beta1}, want: "0.1.9-dev.1"},
		{name: "stable after stable advances patch", channel: "stable", versions: []releaseVersion{stable}, want: "0.1.9"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := nextVersion(test.channel, test.versions); got != test.want {
				t.Fatalf("nextVersion() = %q, want %q", got, test.want)
			}
		})
	}
}

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
		_, _ = w.Write([]byte(`{"channel":"dev","version":"0.1.8-dev.1"}`))
	}))
	defer server.Close()

	manifest, err := fetchManifest(server.Client(), server.URL+"/latest.json")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Channel != "dev" || manifest.Version != "0.1.8-dev.1" {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}

	_, err = fetchManifest(server.Client(), server.URL+"/missing.json")
	if !errors.Is(err, errManifestNotFound) {
		t.Fatalf("missing manifest error = %v", err)
	}
}

func mustParseVersion(t *testing.T, value string) releaseVersion {
	t.Helper()
	version, err := parseVersion(value)
	if err != nil {
		t.Fatal(err)
	}
	return version
}
