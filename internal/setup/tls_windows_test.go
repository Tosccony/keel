package setup

import (
	"os"
	"strings"
	"testing"
)

// Windows sets PROCESSOR_IDENTIFIER for the computer, not per program, so
// it says ARM even inside the x64 emulator.
func TestNeedsNoTLSOnlyOnARM(t *testing.T) {
	arm := strings.Contains(os.Getenv("PROCESSOR_IDENTIFIER"), "ARM")
	if got := needsNoTLS(); got != arm {
		t.Errorf("needsNoTLS() = %v on %q", got, os.Getenv("PROCESSOR_IDENTIFIER"))
	}
}
