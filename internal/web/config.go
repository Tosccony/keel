package web

import (
	"bytes"
	"os"
	"path/filepath"
	"text/template"
)

// The config files Keel writes at every start. Each says so at the top and
// points to the file the user owns for their own changes: custom.conf for
// Apache and php.d/custom.ini for PHP, which Keel creates once and never
// touches again.

// unixModules are the Apache modules XAMPP enables that a PHP course
// uses, for an Apache whose modules are separate files (macOS's own, and
// Linux packages). Keel's bundled builds compile them in instead.
var unixModules = []string{
	"mpm_event", "authz_core", "authz_host", "authz_user", "authn_core",
	"authn_file", "auth_basic", "access_compat", "unixd", "dir", "mime",
	"log_config", "alias", "rewrite", "headers", "setenvif", "env",
	"filter", "deflate", "expires", "proxy", "proxy_fcgi", "autoindex",
	"include", "negotiation", "version", "cgid",
}

var httpdConf = template.Must(template.New("httpd.conf").Parse(`# Written by Keel each time the web server starts: changes here are lost.
# Put your own Apache settings in custom.conf, next to this file.

ServerRoot "{{.Folder}}"
DefaultRuntimeDir "{{.Run}}"
PidFile "{{.Run}}/httpd.pid"
Listen 127.0.0.1:{{.Port}}
ServerName localhost:{{.Port}}
{{range .Modules}}LoadModule {{.}}_module "{{$.ModulesDir}}/mod_{{.}}.so"
{{end}}
ErrorLog "{{.Logs}}/apache-error.log"
LogLevel warn
LogFormat "%h %l %u %t \"%r\" %>s %b \"%{Referer}i\" \"%{User-Agent}i\"" combined
CustomLog "{{.Logs}}/access.log" combined
TypesConfig "{{.MimeTypes}}"

DocumentRoot "{{.Sites}}"
<Directory />
    AllowOverride None
    Require all denied
</Directory>
<Directory "{{.Sites}}">
    Options Indexes FollowSymLinks
    AllowOverride All
    Require all granted
</Directory>
DirectoryIndex index.php index.html index.htm
<Files ".ht*">
    Require all denied
</Files>

# Lets Keel tell its own Apache from another program on the same port.
Header always set X-Keel-Server "{{.Token}}"

# Addresses PHP code can read from $_SERVER.
SetEnv KEEL_MAIL_INBOX "{{.MailInbox}}"
SetEnv KEEL_MARIADB_PORT "{{.MariaDBPort}}"

{{if .PHPModule -}}
LoadModule php_module "{{.PHPModule}}"
PHPIniDir "{{.Conf}}"
<FilesMatch "\.php$">
    SetHandler application/x-httpd-php
</FilesMatch>
{{- else -}}
<FilesMatch "\.php$">
    SetHandler "proxy:fcgi://127.0.0.1:{{.FPMPort}}"
</FilesMatch>
{{- end}}
{{if .PHPMyAdmin}}
Alias /phpmyadmin "{{.PHPMyAdmin}}"
<Directory "{{.PHPMyAdmin}}">
    Options FollowSymLinks
    AllowOverride None
    Require local
</Directory>
{{end}}
Include "{{.Conf}}/custom.conf"
`))

const customConf = `# Your own Apache settings. Keel never changes this file.
# It's read last, so what you put here wins. Keel checks it before
# starting Apache and shows any mistake.
#
# For example, a site at http://localhost:8080/blog served from elsewhere:
#   Alias /blog "/path/to/blog"
#   <Directory "/path/to/blog">
#       AllowOverride All
#       Require all granted
#   </Directory>
`

var phpIni = template.Must(template.New("php.ini").Parse(`; Written by Keel each time the web server starts: changes here are lost.
; Put your own PHP settings in php.d/custom.ini, next to this file.

; Show every error in the page: this is a computer for learning.
display_errors = On
display_startup_errors = On
error_reporting = E_ALL
log_errors = On
error_log = "{{.Logs}}/php-errors.log"

memory_limit = 256M
max_execution_time = 120
max_input_time = 120
post_max_size = 64M
upload_max_filesize = 64M
max_file_uploads = 50
upload_tmp_dir = "{{.Tmp}}"
session.save_path = "{{.Sessions}}"
default_charset = "UTF-8"
{{if .Timezone}}date.timezone = "{{.Timezone}}"
{{end}}
; Code that only gives a database name or "localhost" still reaches
; Keel's MariaDB, wherever its port moved.
mysqli.default_host = "127.0.0.1"
mysqli.default_port = {{.MariaDBPort}}
{{if .MariaDBSocket}}mysqli.default_socket = "{{.MariaDBSocket}}"
pdo_mysql.default_socket = "{{.MariaDBSocket}}"
{{end}}
; Every email goes to Keel's mail catcher; none leaves the computer.
{{if .Sendmail}}sendmail_path = "{{.Sendmail}}"
{{else}}SMTP = 127.0.0.1
smtp_port = {{.MailPort}}
{{end}}
; Changes to PHP files show up on the next request.
opcache.enable = 1
opcache.revalidate_freq = 0
{{if .ExtensionDir}}
extension_dir = "{{.ExtensionDir}}"
{{range .Extensions}}extension = {{.}}
{{end}}{{end}}`))

const customIni = `; Your own PHP settings. Keel never changes this file.
; It's read after Keel's settings, so what you put here wins.
; Restart the web server in Keel after changing it.
;
; For example:
;   memory_limit = 512M
;   display_errors = Off
`

var fpmConf = template.Must(template.New("php-fpm.conf").Parse(`; Written by Keel each time the web server starts: changes here are lost.
[global]
pid = "{{.Run}}/php-fpm.pid"
error_log = "{{.Logs}}/php-fpm.log"
daemonize = no

[www]
listen = 127.0.0.1:{{.FPMPort}}
listen.allowed_clients = 127.0.0.1
pm = dynamic
pm.max_children = 10
pm.start_servers = 2
pm.min_spare_servers = 1
pm.max_spare_servers = 3
catch_workers_output = yes
clear_env = no
`))

// windowsExtensions are the PHP extensions Keel turns on with php.net's
// Windows build, which ships them as separate DLLs: what XAMPP enables,
// and what Laravel, WordPress and phpMyAdmin need.
var windowsExtensions = []string{
	"bz2", "curl", "fileinfo", "gd", "gettext", "intl", "mbstring", "exif",
	"mysqli", "openssl", "pdo_mysql", "pdo_sqlite", "sodium", "sqlite3",
	"zip",
}

// writeConfig writes the generated files, and the user's files if they're
// missing.
func (s *Server) writeConfig() error {
	for _, dir := range []string{s.conf(), s.logs(), s.run(), s.tmp(), s.sessions(), filepath.Join(s.conf(), "php.d")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	v := s.vars()
	files := []struct {
		name string
		tmpl *template.Template
	}{{"httpd.conf", httpdConf}, {"php.ini", phpIni}}
	if s.PHPFPM != "" {
		files = append(files, struct {
			name string
			tmpl *template.Template
		}{"php-fpm.conf", fpmConf})
	}
	for _, f := range files {
		var b bytes.Buffer
		if err := f.tmpl.Execute(&b, v); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(s.conf(), f.name), b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	for name, body := range map[string]string{"custom.conf": customConf, filepath.Join("php.d", "custom.ini"): customIni} {
		p := filepath.Join(s.conf(), name)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				return err
			}
		}
	}
	return nil
}

// vars are the values the templates fill in. Paths use forward slashes,
// which Apache and PHP accept on Windows too, so backslashes never need
// escaping.
func (s *Server) vars() map[string]any {
	slash := filepath.ToSlash
	v := map[string]any{
		"Folder": slash(s.Folder), "Conf": slash(s.conf()), "Logs": slash(s.logs()), "Run": slash(s.run()),
		"Tmp": slash(s.tmp()), "Sessions": slash(s.sessions()), "Sites": slash(s.Sites),
		"Port": s.Port, "FPMPort": s.FPMPort, "MailPort": s.MailPort, "MailInbox": s.MailInbox,
		"MariaDBPort": s.MariaDBPort, "MariaDBSocket": slash(s.MariaDBSocket),
		"MimeTypes": slash(s.MimeTypes), "ModulesDir": slash(s.ModulesDir),
		"PHPModule": slash(s.PHPModule), "Sendmail": s.Sendmail, "Timezone": s.Timezone,
		"PHPMyAdmin": slash(s.PHPMyAdmin), "ExtensionDir": slash(s.ExtensionDir),
		"Token": s.token,
	}
	if s.ModulesDir != "" {
		v["Modules"] = unixModules
	}
	if s.PHPModule != "" && s.ExtensionDir != "" {
		v["Extensions"] = windowsExtensions
	}
	return v
}
