package setup

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// registerPerl tells Apache which program runs .pl and .cgi files.
//
// Scripts from tutorials start with "#!/usr/bin/perl", which doesn't exist
// on Windows, so Keel's Apache ignores that line
// (ScriptInterpreterSource Registry-Strict) and asks the registry instead:
// the ExecCGI command for the file type. Keel sets it for the current user
// only (no admin rights needed), and hides it from Explorer's menus.
func registerPerl(perl string) error {
	for _, ext := range []string{".pl", ".cgi"} {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+ext+`\Shell\ExecCGI`, registry.SET_VALUE)
		if err != nil {
			return err
		}
		k.SetStringValue("ProgrammaticAccessOnly", "")
		k.Close()
		cmd, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\`+ext+`\Shell\ExecCGI\Command`, registry.SET_VALUE)
		if err != nil {
			return err
		}
		err = cmd.SetStringValue("", `"`+perl+`" "%1"`)
		cmd.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// forgetPerl removes what registerPerl wrote, if it still runs a Perl
// inside dir (a portable Keel's folder), so an installed Keel's
// registration is left alone. The .pl and .cgi keys themselves go only if
// nothing else is in them.
func forgetPerl(dir string) {
	prefix := strings.ToLower(`"` + strings.TrimRight(dir, `\`) + `\`)
	for _, ext := range []string{".pl", ".cgi"} {
		base := `Software\Classes\` + ext
		k, err := registry.OpenKey(registry.CURRENT_USER, base+`\Shell\ExecCGI\Command`, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		cmd, _, err := k.GetStringValue("")
		k.Close()
		if err != nil || !strings.HasPrefix(strings.ToLower(cmd), prefix) {
			continue
		}
		registry.DeleteKey(registry.CURRENT_USER, base+`\Shell\ExecCGI\Command`)
		registry.DeleteKey(registry.CURRENT_USER, base+`\Shell\ExecCGI`)
		for _, key := range []string{base + `\Shell`, base} {
			if keyIsEmpty(key) {
				registry.DeleteKey(registry.CURRENT_USER, key)
			}
		}
	}
}

// keyIsEmpty reports whether a key of the current user has no subkeys and no
// values.
func keyIsEmpty(key string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return false
	}
	defer k.Close()
	info, err := k.Stat()
	return err == nil && info.SubKeyCount == 0 && info.ValueCount == 0
}
