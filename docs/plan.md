# Reaching XAMPP's feature set

Keel replaces XAMPP for college courses, so it needs everything XAMPP
offers, and must do each part more safely and more simply.

## Decisions

| | Choice |
|---|---|
| PHP | **8.4**, one version (supported until the end of 2028; phpMyAdmin 5.2.3 runs cleanly on it). Several versions to choose from: parked (2026-11-18) |
| Database | MariaDB 11.8 LTS ([bundled-mariadb.md](bundled-mariadb.md)) |
| Extras | All of XAMPP's: a mail catcher (in place of Mercury), Perl, an FTP server, and Tomcat |
| Websites folder (XAMPP's htdocs) | Asked on first run; the default is `Documents/Keel/htdocs` on every system |
| Portable | A Windows zip (`Keel-Portable-x64.zip`) for external drives. `portable.txt` beside `keel.exe` keeps the data (`Keel\data`) and the websites (`Keel\htdocs`) in that folder, a websites folder inside it is saved relative so a new drive letter doesn't matter, and the Visual C++ runtime travels with it. No install, no admin rights |
| Ports | Web 8080 (HTTPS 8443), MariaDB 3306, mail 1025 (inbox page 8025), FTP 2121, Tomcat 8081. Each moves to the next free port if taken, says which program had it, and remembers the new port |

## XAMPP's parts, and Keel's

| XAMPP | Keel | How |
|---|---|---|
| Apache | Apache 2.4 | Windows: Apache Lounge's build. macOS and Linux: built from source with the modules XAMPP enables compiled in, so there's nothing to relocate. Keel writes `httpd.conf` on every start (ports and paths) and includes a file the user owns for their own changes |
| PHP | PHP 8.4 | Windows: php.net's thread-safe build, loaded into Apache (like XAMPP). macOS and Linux: a static build (static-php-cli, from source) running as PHP-FPM behind Apache. Keel writes the settings XAMPP users expect (errors shown, uploads up to 64 MB, mail to the mail catcher) and includes a user-owned `php.ini` part |
| MariaDB | MariaDB 11.8 | Done: crash-safe stop, recovery, repair, backups |
| phpMyAdmin | phpMyAdmin 5.2.3 | Served at `/phpmyadmin`, configured by Keel on every start with MariaDB's current port, so it keeps working after a port move |
| Mercury (mail server) | Mail catcher | Built into Keel (Go). Catches every email PHP sends and shows it on a local inbox page; nothing leaves the computer. PHP sends to it over SMTP on Windows, and through a small `sendmail` stand-in on macOS and Linux |
| FileZilla Server | FTP server | Built into Keel (Go). Serves the websites folder, with a username and password set in Keel |
| Perl | Perl 5 for CGI | Windows: Strawberry Perl, downloaded when first turned on (it's large). macOS and Linux: the system's Perl. Apache runs `.pl` and `.cgi` files in `cgi-bin` |
| Tomcat | Tomcat 10.1 | Downloaded when first turned on, with a Java runtime if the computer has none. Started and stopped through Tomcat's own scripts |
| XAMPP Portable | Portable zip | See Portable above |
| Control panel: Start/Stop | A card per part | Each part has a status, ports, Start/Stop, and a menu |
| Admin buttons | "Open" in each card | The site, phpMyAdmin, the mail inbox, Tomcat's page |
| Config buttons | "Settings" in each menu | Opens the user-owned config file in the system's editor |
| Logs buttons | "Logs" in each menu | Opens the part's log |
| Explorer | "Open websites folder" | |
| Shell | "Open terminal" | A terminal in the websites folder, with `mariadb`, `php` and `perl` on its PATH |
| Netstat | "Ports" | Every listening port and the program holding it |
| Services (run as Windows services) | Background service | Starts the parts at sign-in and stops MariaDB cleanly when the computer shuts down |
| Autostart | "Start when Keel opens" per part | |

## Order of work

1. ✅ **Several parts in one window**, settings, the first-run question about the websites folder.
2. ✅ **Mail catcher** and **FTP server**.
3. ✅ **Apache and PHP**, then **phpMyAdmin**.
4. **Bundled runtimes and installers.** ✅ MariaDB, Apache, PHP and phpMyAdmin are built and tested for all three systems in CI (`mariadb.yml`, `web.yml`). ✅ The installers include them (`scripts/fetch-runtimes.sh`), with the Visual C++ runtime on Windows. ✅ The runtime builds are published as pre-releases (`mariadb-11.8.9`, `web-2.4.70-php8.4.27`), and v1.0.0 is Keel's first release with them inside.
5. ✅ **Perl** and **Tomcat**, downloaded when first turned on.
6. **Control-panel extras.** ✅ Ports view, Terminal, logs in each menu, autostart, the tray icon, opening at sign-in, Settings. ✅ A system shutdown or sign-out now counts as a clean stop when MariaDB's own log confirms it, which covers most of what the background service was for.
7. **Loose ends.** ✅ Windows on ARM (MariaDB runs without TLS there). ✅ A root password set by the student no longer gets in Keel's way: Keel logs in with its own account, and stops MariaDB through its shutdown event on Windows if even that fails. ✅ The `mariadb` command in Keel's terminal reaches Keel's MariaDB. ✅ HTTPS on port 8443 with a certificate Keel makes for this computer (browsers warn once, as with XAMPP's). ✅ `mariadb-upgrade` runs by itself after an update brings a newer MariaDB.

8. **Portable, icons and Windows checks.** ✅ The portable Windows zip. ✅ `keel.exe` carries the Keel icon and version details; the installer adds a desktop icon by default; the macOS disk image shows the Keel icon. ✅ Keel's tests run on Windows x64 and Windows on ARM on every push, and every release is installed, and run portable from an exFAT drive, on a Windows machine before it's published.

Each step ships with tests and is committed on its own.

## After 1.0

From the security review (2026-11-18). Each one matters most on a computer that several people use at once.

- **MariaDB's file access.** Add `--secure-file-priv` with a folder of Keel's, so another user signed in at the same time can't use root (no password, like XAMPP) to write files as the student. Course code that runs `LOAD DATA INFILE` or `SELECT ... INTO OUTFILE` on other folders would then need that folder; `LOAD DATA LOCAL INFILE` and phpMyAdmin's Import keep working. Needs a decision first.
- **PHP-FPM on a private socket** (macOS and Linux) instead of port 9000, which any user on the computer can reach.
- **FTP transfer ports on 127.0.0.1 only.** The FTP library opens them on every network card. Another computer still can't take over a transfer, but Windows shows a firewall prompt.
- **Pin the sources static-php-cli downloads** when it builds PHP for macOS and Linux.
- **Code signing** for the Windows installer and the Mac app, which also removes the first-launch warnings.
