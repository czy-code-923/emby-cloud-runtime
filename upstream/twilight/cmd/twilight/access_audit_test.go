package main

import (
	"reflect"
	"testing"

	"github.com/prejudice-studio/twilight/internal/config"
)

func TestWhitelistAuditReadsMultiLineOrigins(t *testing.T) {
	lines := []config.Line{{URL: "https://ONE.example:443/"}, {URL: "https://two.example"}, {URL: "https://one.example"}, {URL: "http://private:8096"}, {URL: "https://secret@bad.example"}, {URL: "https://bad.example/?key=secret"}, {URL: "https://bad.example/path"}}
	if got := auditPlaybackLines("", lines); !reflect.DeepEqual(got, []string{"https://one.example", "https://two.example"}) {
		t.Fatalf("unexpected multi-line origins: %v", got)
	}
	if got := auditPlaybackLines("https://legacy.example/", lines[:1]); !reflect.DeepEqual(got, []string{"https://legacy.example", "https://one.example"}) {
		t.Fatalf("legacy origin lost: %v", got)
	}
	if err := runWhitelistAccessAudit([]string{"--confirm", "WRONG"}); err == nil {
		t.Fatal("audit accepted missing explicit confirmation")
	}
}
