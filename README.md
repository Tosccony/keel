# Keel

A local web stack for Windows, macOS and Linux: Apache, PHP, MariaDB and phpMyAdmin, run from one small control window.

- **Stops the database cleanly.** It asks MariaDB to shut down and waits, instead of killing the process.
- **Picks a free port on its own.** It starts on 8080. If that port is taken, it moves to the next free one and tells you.
- **No accounts.** No sign-in, no telemetry.

> **Status: early.** Releases currently contain a placeholder window while the real control panel is built.

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
go test ./...                         # tests start real MariaDB servers; skipped if MariaDB isn't installed
go run ./cmd/keel-lab start           # start MariaDB from ~/personal/keel-lab on port 3307
go run ./cmd/keel                     # run the app
scripts/package-macos.sh 0.1.0        # macOS: dist/Keel-macOS.dmg
scripts/package-linux.sh 0.1.0        # Linux: dist/*.deb, *.rpm, *.tar.gz (needs nfpm)
```

On Windows, build `dist\windows\keel.exe`, then run `iscc packaging\windows\keel.iss` (Inno Setup 6) to get `dist\Keel-Setup-x64.exe`.

## Layout

| Path | What it is |
|---|---|
| `cmd/keel/` | App entry point (placeholder window for now) |
| `internal/mariadb/` | Starts MariaDB safely: its own process group, a port check, and a readiness ping. It never kills the server |
| `cmd/keel-lab/` | Developer tool for trying the server code against a local MariaDB. Not shipped |
| `packaging/windows/` | Inno Setup script for the `.exe` installer |
| `packaging/macos/` | `Info.plist` for `Keel.app` |
| `packaging/linux/` | `.desktop` entry and nfpm config for `.deb` and `.rpm` |
| `scripts/` | Packaging scripts the release workflow runs |
| `install.sh`, `install.ps1` | One-line installers for the latest release |
| `.github/workflows/release.yml` | Tag-triggered build and release |
