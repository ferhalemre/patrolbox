package discover

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/ferhalemre/patrolbox/internal/config"
)

// TestSmokeHost prints what discovery finds on the real machine.
// Run with: PATROLBOX_SMOKE=1 go test -run TestSmokeHost -v ./internal/discover/
func TestSmokeHost(t *testing.T) {
	if os.Getenv("PATROLBOX_SMOKE") == "" {
		t.Skip("set PATROLBOX_SMOKE=1")
	}
	for _, s := range Sites(context.Background(), config.Default().Discover, "/var/run/docker.sock") {
		fmt.Printf("%-40s %s\n", s.Host, s.Source)
	}
}
