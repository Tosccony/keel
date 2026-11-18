package setup

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// A portable Keel removes its Perl registration when the web server stops,
// but never one that runs a Perl elsewhere (an installed Keel's).
func TestForgetPerlOnlyRemovesItsOwn(t *testing.T) {
	const key = `Software\Classes\.pl\Shell\ExecCGI\Command`
	registered := func() bool {
		k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
		if err != nil {
			return false
		}
		k.Close()
		return true
	}
	if registered() {
		t.Skip("this account already has a Perl registration; not touching it")
	}
	t.Cleanup(func() { forgetPerl(`C:\Elsewhere`) })

	if err := registerPerl(`C:\Elsewhere\perl\bin\perl.exe`); err != nil {
		t.Fatal(err)
	}
	forgetPerl(`K:\Keel`)
	if !registered() {
		t.Fatal("removed a registration that runs another folder's Perl")
	}
	forgetPerl(`C:\Elsewhere`)
	if registered() {
		t.Fatal("left its own registration behind")
	}
	if _, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\.pl`, registry.QUERY_VALUE); err == nil {
		t.Error("left an empty .pl key behind")
	}
}
