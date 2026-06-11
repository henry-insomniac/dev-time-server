package buildinfo_test

import (
	"testing"

	"github.com/henry-insomniac/dev-time-server/internal/buildinfo"
)

func TestServiceNameIdentifiesServer(t *testing.T) {
	if buildinfo.ServiceName() != "dev-time-server" {
		t.Fatalf("expected dev-time-server, got %q", buildinfo.ServiceName())
	}
}
