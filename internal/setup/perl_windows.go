package setup

import (
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
