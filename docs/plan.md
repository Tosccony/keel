# Reaching XAMPP's feature set

Keel replaces XAMPP for college courses, so it needs everything XAMPP
offers, and must do each part more safely and more simply.

## Decisions

| | Choice |
|---|---|
| PHP | **8.4**, one version (supported until the end of 2028; phpMyAdmin 5.2.3 runs cleanly on it) |
| Database | MariaDB 11.8 LTS ([bundled-mariadb.md](bundled-mariadb.md)) |
| Extras | All of XAMPP's: a mail catcher (in place of Mercury), Perl, an FTP server, and Tomcat |
| Websites folder (XAMPP's htdocs) | Asked on first run; the default is `Documents/Keel/htdocs` on every system |
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
4. **Bundled runtimes and installers.** ✅ MariaDB, Apache, PHP and phpMyAdmin are built and tested for all three systems in CI (`mariadb.yml`, `web.yml`). Next: publish them and put them into the installers, with the Visual C++ runtime on Windows.
5. ✅ **Perl** and **Tomcat**, downloaded when first turned on.
6. **Control-panel extras.** ✅ Ports view, Terminal, logs in each menu, autostart, the tray icon, opening at sign-in, Settings. ✅ A system shutdown or sign-out now counts as a clean stop when MariaDB's own log confirms it, which covers most of what the background service was for.
7. **Loose ends**: TLS on Windows on ARM; stopping MariaDB on Windows after a student sets a root password; HTTPS for the web server.

Each step ships with tests and is committed on its own.
