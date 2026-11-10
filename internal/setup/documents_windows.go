package setup

import "golang.org/x/sys/windows"

// documentsDir asks Windows where Documents is: it may have been moved, to
// OneDrive for example.
func documentsDir() string {
	if dir, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0); err == nil && dir != "" {
		return dir
	}
	return homeDocuments()
}
