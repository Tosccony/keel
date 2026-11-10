# Keel

A local web stack for Windows, macOS and Linux: Apache, PHP, MariaDB and phpMyAdmin, run from one small control window.

- **Stops the database cleanly.** It asks MariaDB to shut down and waits, instead of killing the process.
- **Picks a free port on its own.** It starts on 8080. If that port is taken, it moves to the next free one and tells you.
- **No accounts.** No sign-in, no telemetry.

> **Status: early.** Every part XAMPP offers works in a development build: Apache with PHP 8.4, MariaDB (with crash-safe stopping, Repair, Back up and Restore), phpMyAdmin, a mail catcher, an FTP server, Tomcat and Perl. Putting the bundled runtimes into the installers is next (see [docs/plan.md](docs/plan.md)); until then a development build uses programs you install yourself, with its files in `~/personal/keel-lab`.

## Install

Download the latest version:

| System | Download |
|---|---|
| Windows 10/11 (x64; Windows 11 on ARM runs it through emulation) | [Keel-Setup-x64.exe](https://github.com/Tosccony/keel/releases/latest/download/Keel-Setup-x64.exe) |
| macOS 12 or later (Apple Silicon and Intel) | [Keel-macOS.dmg](https://github.com/Tosccony/keel/releases/latest/download/Keel-macOS.dmg) |
| Ubuntu, Debian, Mint | [keel_amd64.deb](https://github.com/Tosccony/keel/releases/latest/download/keel_amd64.deb) |
| Fedora, openSUSE | [keel_x86_64.rpm](https://github.com/Tosccony/keel/releases/latest/download/keel_x86_64.rpm) |
| Other Linux (x86_64) | [keel_linux_x86_64.tar.gz](https://github.com/Tosccony/keel/releases/latest/download/keel_linux_x86_64.tar.gz) |

Or install from a terminal.

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/Tosccony/keel/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/Tosccony/keel/main/install.ps1 | iex
```

### First launch

The installers aren't code-signed yet, so the first launch shows a warning:

- **Windows:** SmartScreen may say "Windows protected your PC". Click **More info**, then **Run anyway**.
- **macOS:** If you downloaded the .dmg in a browser, macOS blocks the first open. Open **System Settings > Privacy & Security**, scroll down, and click **Open Anyway**. The install script doesn't trigger this.

## Releasing

GitHub Actions builds every release (`.github/workflows/release.yml`).

1. Push your changes to `main`.
2. Tag the version and push the tag:

   ```sh
   git tag v0.1.0
   git push origin v0.1.0
   ```

3. Actions builds the Windows `.exe`, the macOS `.dmg`, and the Linux `.deb`, `.rpm` and `.tar.gz`. It adds `SHA256SUMS.txt` and publishes a GitHub release. The "latest" download links above then point to the new release automatically.

Tags with a hyphen, like `v0.2.0-beta.1`, become pre-releases and don't replace "latest".

To test the builds without publishing, open **Actions > Release > Run workflow**. The installers appear as artifacts on that run.

## Build locally

You need Go (the version in `go.mod`) and a C compiler, because the window toolkit ([Fyne](https://fyne.io)) uses cgo:

- **macOS:** Xcode Command Line Tools
- **Linux:** `gcc libgl1-mesa-dev xorg-dev libxkbcommon-dev libwayland-dev wayland-protocols`
- **Windows:** MSYS2 with `mingw-w64-x86_64-gcc`

Then:

```sh
go test -timeout 30m ./...            # tests start real MariaDB servers (skipped if MariaDB isn't installed); a busy machine needs more than the default 10 minutes
go run ./cmd/keel-lab start           # start MariaDB from ~/personal/keel-lab on port 3307
go run ./cmd/keel-lab stop            # stop it the clean way (also: init, status, check)
go run ./cmd/keel-lab repair          # fix damage that stops MariaDB starting (also: backup, backups, restore NAME)
go run ./cmd/keel                     # run the app
KEEL_SCREENSHOT=/tmp go test ./cmd/keel -run Screenshot   # pictures of the window, light and dark
scripts/make-icons.sh                 # rebuild the app icons after changing cmd/keel/icon.svg
scripts/package-macos.sh 0.1.0        # macOS: dist/Keel-macOS.dmg
scripts/package-linux.sh 0.1.0        # Linux: dist/*.deb, *.rpm, *.tar.gz (needs nfpm)
```

On Windows, build `dist\windows\keel.exe`, then run `iscc packaging\windows\keel.iss` (Inno Setup 6) to get `dist\Keel-Setup-x64.exe`.

## Layout

| Path | What it is |
|---|---|
| `cmd/keel/` | The Keel window: a card per part (web server, MariaDB, mail catcher, FTP server, Tomcat) with Start/Stop and a menu, the tray icon, Settings, Terminal, Ports, and the activity log. `icon.svg` is the logo; `theme.go` holds the light and dark colours |
| `internal/setup/` | Which MariaDB Keel runs and where its files live: the bundled one and the user's app data folder in an installed Keel, or the lab in a development build. Creates the data folder on the first start, and remembers a port Keel moved to in `keel-settings.json` |
| `internal/ports/` | Finds free ports, names the program holding a taken one, and lists every listening port |
| `internal/web/` | The web server: Apache with PHP (PHP-FPM on macOS and Linux, a module on Windows), config written at every start, phpMyAdmin, Perl CGI, the welcome page |
| `internal/mail/` | The mail catcher: a receive-only SMTP server, the inbox page, and the sendmail stand-in |
| `internal/ftp/` | The FTP server, serving the websites folder |
| `internal/tomcat/` | Tomcat, run from Keel's own folder |
| `internal/proc/` | Starts Apache, PHP-FPM and Tomcat as their own processes and finds them again |
| `internal/fetch/` | Checked downloads for the parts installed when first turned on |
| `internal/extras/` | The pinned versions of those parts: Tomcat, Java, Strawberry Perl |
| `internal/mariadb/` | Starts and stops MariaDB safely, and recovers from unclean shutdowns: a crash journal, a safety copy before recovery, a check of every table, Repair, Back up and Restore, and a lock so two Keels never act on the same data folder at once |
| `cmd/keel-lab/` | Command-line tool for trying the server code (start, stop, check, repair, backup, restore). Not shipped |
| `packaging/windows/` | Inno Setup script and icon for the `.exe` installer |
| `packaging/macos/` | `Info.plist` and icon for `Keel.app` |
| `packaging/linux/` | `.desktop` entry, icon, and nfpm config for `.deb` and `.rpm` |
| `scripts/` | Packaging scripts the release workflow runs; the runtime scripts (MariaDB: `fetch-mariadb.sh`, `build-mariadb.sh`, `trim-mariadb.sh`, `test-mariadb-runtime.sh`; web: `build-apache.sh`, `build-php.sh`, `fetch-web-windows.sh`, `fetch-phpmyadmin.sh`, `test-web-runtime.sh`, `merge-macos.sh`); and `make-icons.sh` |
| `packaging/mariadb/mariadb.env`, `packaging/web/web.env`, `packaging/phpmyadmin/phpmyadmin.env` | The versions Keel ships, with the checksum of every download |
| `install.sh`, `install.ps1` | One-line installers for the latest release |
| `.github/workflows/release.yml` | Tag-triggered build and release |
| `.github/workflows/mariadb.yml`, `web.yml` | Prepare and test the MariaDB, Apache, PHP and phpMyAdmin that Keel ships, on all three systems (run by hand) |
| `docs/testing.md` | Every test case, automated and still to do |
| `docs/plan.md` | Everything XAMPP offers, how Keel does each, and the order of work |
| `docs/bundled-mariadb.md` | How Keel ships MariaDB, and why |
