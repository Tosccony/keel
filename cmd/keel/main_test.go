package main

import (
	"os"
	"testing"
)

// TestMain lets the test binary stand in for Keel as PHP's sendmail, the
// way the real program does, so a test can send mail from PHP.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--sendmail" {
		os.Exit(runSendmail(os.Args[2:]))
	}
	os.Exit(m.Run())
}
