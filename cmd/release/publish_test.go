package main

import (
	"strings"
	"testing"
)

func TestParsePublishOptionsRequiresUploadSkipWithNotarySkip(t *testing.T) {
	t.Parallel()

	_, err := parsePublishOptions([]string{"1.2.3-dev.1", "--skip-notarize"})
	if err == nil || !strings.Contains(err.Error(), "requires --skip-upload") {
		t.Fatalf("parsePublishOptions() error = %v", err)
	}
}

func TestParsePublishOptionsAllowsLocalUnnotarizedBuild(t *testing.T) {
	for _, name := range []string{"MACOS_CERTIFICATE", "MACOS_CERTIFICATE_PWD", "MACOS_SIGN_IDENTITY"} {
		t.Setenv(name, "test")
	}

	options, err := parsePublishOptions([]string{"1.2.3-dev.1", "--skip-notarize", "--skip-upload"})
	if err != nil {
		t.Fatal(err)
	}
	if !options.skipNotarize || !options.skipUpload {
		t.Fatalf("unexpected options: %#v", options)
	}
}
