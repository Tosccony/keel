# Shipping MariaDB inside Keel

An installed Keel carries its own MariaDB, so it works on a lab PC with no
internet and nothing else installed. This page records how, and why.

## Decisions

| Question | Choice | Why |
|---|---|---|
| Bundle, or download on first run? | **Bundle** in every installer | College lab machines often have restricted internet, and instructors install from images |
| Which MariaDB? | **11.8 LTS** (`packaging/mariadb/mariadb.env`) | Supported until February 2030, the longest of the current releases, and the version Debian ships |
| Windows | MariaDB's **official build**, trimmed | Self-contained apart from Windows 10 1903+'s own ICU library and the Visual C++ runtime, which MariaDB ships beside it |
| Linux | MariaDB's **official build**, trimmed, with debugging symbols stripped | Needs only glibc 2.28+ (Ubuntu 20.04, Debian 10, RHEL 8 and later), `libcrypt.so.1` and `libsystemd.so.0`. Fedora puts `libcrypt.so.1` in `libxcrypt-compat`, so the `.rpm` must require it |
| macOS | **Built from MariaDB's source** for Apple Silicon and Intel, joined into one universal build | MariaDB publishes no macOS builds. SSL, zlib, PCRE and fmt come from MariaDB's own source tree, so the build doesn't depend on Homebrew |
| Where the data lives | `~/Library/Application Support/Keel/mariadb` (macOS), `%LOCALAPPDATA%\Keel\mariadb` (Windows), `~/.local/share/keel/mariadb` (Linux) | Per user. On Windows it's never the roaming AppData folder: college networks copy that to a server at sign-out, and a database must not be copied while in use |
| Port | 3306, or the next free one (see `internal/setup`) | What PHP code and phpMyAdmin expect |
| First start | Keel creates the data folder itself (`mariadb-install-db`); root has no password, like XAMPP | Nothing to set up by hand |

## What's kept

`scripts/trim-mariadb.sh` keeps an allow-list, so a new MariaDB release can't
slip test programs into Keel:

- **bin/**: `mariadbd`, setup (`mariadb-install-db`, plus `mysqld.exe` on Windows, which the Windows setup tool runs), the repair tools (`aria_chk`, `myisamchk`), the client programs (`mariadb`, `mariadb-dump`, `mariadb-admin`, `mariadb-check`, `mariadb-import`), and `mariadb-upgrade` for moving to a newer MariaDB.
- **share/**: every language's error messages, the character sets, and the SQL a new data folder is set up with.
- **lib/plugin/**: login plugins and a few small ones. Storage engines that need other libraries (CONNECT, RocksDB, Spider, Mroonga, S3, ColumnStore) are left out.
- MariaDB's licence (GPL v2), README and third-party notices.

Sizes for 11.8.9, compressed: Windows 16.5 MB (from 104 MB), Linux 34.7 MB (from 492 MB).

## Tested so far

| System | How | Result |
|---|---|---|
| Ubuntu 24.04, Debian 12, Fedora 41 | Docker (x86_64), Keel's own code: set up, start, check, stop, back up, repair | All pass. Fedora needs `libxcrypt-compat` |
| Windows 11 on ARM (Parallels, x64 emulation) | Same | The server can't start with TLS ("Failed to setup SSL"): the emulator lacks an instruction MariaDB's Windows SSL library uses. Keel detects Windows on ARM and starts MariaDB without TLS; everything else works |
| Real x64 Windows, Linux and macOS | `.github/workflows/mariadb.yml`, which runs `scripts/test-mariadb-runtime.sh` (including a password login over TLS, and a stop with that password) | All pass |

## Why TLS matters

MariaDB 11.4 and later make their own TLS certificate at start, and
MariaDB's command-line client insists on TLS once root has a password:
against a server without TLS it refuses with "SSL is required, but the
server does not support it". So turning TLS off would break students who
set a root password, and Keel keeps it on.

Windows on ARM (Surface, Snapdragon laptops) is the exception: MariaDB
can't start with TLS there, so Keel starts it with `--skip-ssl`. Keel's
terminal points MariaDB's commands at a client settings file Keel writes
(`client/my.cnf` in Keel's folder, through `MARIADB_HOME`) that says
`skip-ssl`, so password logins work there too. The same file gives them
Keel's port, and on macOS and Linux its socket.

## Keel's own account

Keel logs in to MariaDB to confirm a start, to ask it to shut down, and to
check the tables after a crash. Courses often have students give root a
password, which would lock Keel out. So on the first start Keel adds its
own account, `keel@127.0.0.1`, with a random password kept in
`keel-account` next to the data folder (readable only by the user), and
logs in with that. It's remade if the password file goes missing while
root has no password. Without any login, Keel still confirms a start from
MariaDB's log, and stops it with SIGTERM (macOS, Linux) or MariaDB's
`MySQLShutdown<PID>` event (Windows), which need no account.

## Licence

MariaDB is GPL v2. Keel runs it as a separate program, and ships its
licence beside it. The release of each MariaDB build Keel ships carries
MariaDB's source code, which covers the macOS build Keel compiles itself.
