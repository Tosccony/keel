// Package tomcat runs Apache Tomcat, XAMPP's Java web server, for JSP and
// servlet exercises.
//
// Tomcat's own install (CATALINA_HOME) is left untouched; Keel runs it
// from a folder of its own (CATALINA_BASE) with a server.xml it writes at
// every start: HTTP on 127.0.0.1 only, and no shutdown port, since Keel
// stops it with a signal like Apache.
package tomcat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"text/template"
	"time"

	"github.com/Tosccony/keel/internal/proc"
)

// Server is Tomcat, run from Base.
type Server struct {
	Home string // Tomcat's install (CATALINA_HOME)
	Base string // Keel's Tomcat folder (CATALINA_BASE): conf/, logs/, webapps/, temp/, work/
	Java string // the java program
	Port int    // HTTP, on 127.0.0.1

	ReadyTimeout time.Duration // default 60s: Tomcat can be slow the first time

	p *proc.Process
}

// Installed reports whether Tomcat's install is there.
func (s *Server) Installed() bool {
	_, err := os.Stat(filepath.Join(s.Home, "bin", "bootstrap.jar"))
	return err == nil
}

// Webapps is where web applications go: a folder or .war file per app.
func (s *Server) Webapps() string { return filepath.Join(s.Base, "webapps") }

// Log is Tomcat's main log.
func (s *Server) Log() string { return filepath.Join(s.Base, "logs", "tomcat-output.log") }

func (s *Server) process() *proc.Process {
	if s.p == nil {
		sep := string(os.PathListSeparator)
		s.p = &proc.Process{
			Path: s.Java,
			Args: []string{
				"-Dcatalina.home=" + s.Home,
				"-Dcatalina.base=" + s.Base,
				"-Djava.io.tmpdir=" + filepath.Join(s.Base, "temp"),
				"-Djava.util.logging.config.file=" + filepath.Join(s.Base, "conf", "logging.properties"),
				"-Djava.util.logging.manager=org.apache.juli.ClassLoaderLogManager",
				"-cp", filepath.Join(s.Home, "bin", "bootstrap.jar") + sep + filepath.Join(s.Home, "bin", "tomcat-juli.jar"),
				"org.apache.catalina.startup.Bootstrap", "start",
			},
			Dir:     s.Base,
			LogFile: s.Log(),
			PIDFile: filepath.Join(s.Base, "temp", "tomcat.pid"),
			Marker:  "-Dcatalina.base=" + s.Base,
		}
	}
	return s.p
}

// Running reports whether Tomcat is running.
func (s *Server) Running() bool { return s.process().PID() != 0 }

// Start sets up the folder on the first run, writes server.xml, starts
// Tomcat, and returns once it answers on Port.
func (s *Server) Start(ctx context.Context) error {
	if s.Running() {
		return nil
	}
	if !s.Installed() {
		return errors.New("Tomcat isn't installed")
	}
	if err := s.prepare(); err != nil {
		return fmt.Errorf("set up Tomcat's folder: %w", err)
	}
	p := s.process()
	if err := p.Start(); err != nil {
		return err
	}
	if err := s.waitReady(ctx, p); err != nil {
		p.Stop(context.Background(), 10*time.Second)
		return fmt.Errorf("Tomcat didn't start: %w", err)
	}
	return nil
}

// Stop stops Tomcat.
func (s *Server) Stop(ctx context.Context) error {
	err := s.process().Stop(ctx, 20*time.Second)
	if errors.Is(err, proc.ErrNotRunning) {
		return nil
	}
	return err
}

// prepare copies Tomcat's default config into Base the first time, gives
// Base a welcome app, and writes server.xml.
func (s *Server) prepare() error {
	for _, d := range []string{"conf", "logs", "temp", "work", "webapps"} {
		if err := os.MkdirAll(filepath.Join(s.Base, d), 0o755); err != nil {
			return err
		}
	}
	for _, f := range []string{"web.xml", "context.xml", "catalina.properties", "logging.properties", "tomcat-users.xml", "catalina.policy"} {
		dst := filepath.Join(s.Base, "conf", f)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := copyFile(filepath.Join(s.Home, "conf", f), dst); err != nil {
			return err
		}
	}
	root := filepath.Join(s.Webapps(), "ROOT")
	if _, err := os.Stat(root); os.IsNotExist(err) {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, "index.jsp"), []byte(welcomeJSP), 0o644); err != nil {
			return err
		}
	}
	f, err := os.Create(filepath.Join(s.Base, "conf", "server.xml"))
	if err != nil {
		return err
	}
	defer f.Close()
	return serverXML.Execute(f, map[string]any{"Port": s.Port})
}

// waitReady waits until Tomcat answers HTTP on Port.
func (s *Server) waitReady(ctx context.Context, p *proc.Process) error {
	timeout := s.ReadyTimeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := &http.Client{Timeout: 2 * time.Second}
	url := "http://127.0.0.1:" + strconv.Itoa(s.Port) + "/"
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if resp, err := client.Do(req); err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			select {
			case <-p.Exited(): // something else answered, and Tomcat gave up
			default:
				return nil
			}
		}
		select {
		case <-p.Exited():
			return fmt.Errorf("it exited (%v); see %s", p.ExitErr(), s.Log())
		case <-ctx.Done():
			return fmt.Errorf("nothing answered on port %d; see %s", s.Port, s.Log())
		case <-tick.C:
		}
	}
}

// JavaVersion returns the major version of a java program, like 21, or 0.
func JavaVersion(java string) int {
	out, err := exec.Command(java, "-version").CombinedOutput()
	if err != nil {
		return 0
	}
	// openjdk version "21.0.13" 2025-10-21  |  java version "1.8.0_401"
	m := regexp.MustCompile(`version "(\d+)(?:\.(\d+))?`).FindStringSubmatch(string(out))
	if m == nil {
		return 0
	}
	major, _ := strconv.Atoi(m[1])
	if major == 1 && m[2] != "" { // Java 8 and older call themselves 1.x
		major, _ = strconv.Atoi(m[2])
	}
	return major
}

// TomcatVersion reads the version from Tomcat's install, like "10.1.61".
func (s *Server) TomcatVersion() string {
	b, err := os.ReadFile(filepath.Join(s.Home, "RELEASE-NOTES"))
	if err != nil {
		return ""
	}
	if m := regexp.MustCompile(`Apache Tomcat Version (\d+\.\d+\.\d+)`).FindSubmatch(b); m != nil {
		return string(m[1])
	}
	return ""
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

var serverXML = template.Must(template.New("server.xml").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!-- Written by Keel each time Tomcat starts: changes here are lost.
     No shutdown port: Keel stops Tomcat with a signal. -->
<Server port="-1" shutdown="SHUTDOWN">
  <Listener className="org.apache.catalina.startup.VersionLoggerListener" />
  <Listener className="org.apache.catalina.core.JreMemoryLeakPreventionListener" />
  <Service name="Catalina">
    <Connector port="{{.Port}}" address="127.0.0.1" protocol="HTTP/1.1"
               connectionTimeout="20000" URIEncoding="UTF-8" />
    <Engine name="Catalina" defaultHost="localhost">
      <Host name="localhost" appBase="webapps" unpackWARs="true" autoDeploy="true">
        <Valve className="org.apache.catalina.valves.AccessLogValve" directory="logs"
               prefix="access" suffix=".log" pattern="%h %l %u %t &quot;%r&quot; %s %b" />
      </Host>
    </Engine>
  </Service>
</Server>
`))

const welcomeJSP = `<%@ page contentType="text/html; charset=UTF-8" %>
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Tomcat is running</title>
<style>body{font:15px/1.5 system-ui,sans-serif;max-width:640px;margin:40px auto;padding:0 20px}code{background:#eee;padding:1px 5px;border-radius:4px}</style>
</head>
<body>
<h1>Tomcat is running</h1>
<p><%= application.getServerInfo() %> on Java <%= System.getProperty("java.version") %>.</p>
<p>Put each web application in Keel's <code>webapps</code> folder (Tomcat's menu in Keel opens it):
a folder or a <code>.war</code> file called <code>shop</code> opens at <code>/shop</code>.</p>
<p>This page is <code>webapps/ROOT/index.jsp</code>; replace it whenever you like.</p>
</body>
</html>
`
