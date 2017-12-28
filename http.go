package main

import (
	"crypto/tls"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/acme/autocert"
)

type Server struct {
	Handler http.Handler
	Opts    *Options
}

func (s *Server) ListenAndServe() {
	if s.Opts.RedirectHttpToHttps {
		go s.ServeHTTPSRedirector()
	}
	if s.Opts.TLSKeyFile != "" || s.Opts.TLSCertFile != "" || s.Opts.LetsEncryptEnabled {
		s.ServeHTTPS()
	} else {
		s.ServeHTTP()
	}
}

func (s *Server) ServeHTTP() {
	u, err := url.Parse(s.Opts.HttpAddress)
	if err != nil {
		log.Fatalf("FATAL: could not parse %#v: %v", s.Opts.HttpAddress, err)
	}

	var networkType string
	switch u.Scheme {
	case "", "http":
		networkType = "tcp"
	default:
		networkType = u.Scheme
	}
	listenAddr := strings.TrimPrefix(u.String(), u.Scheme+"://")

	listener, err := net.Listen(networkType, listenAddr)
	if err != nil {
		log.Fatalf("FATAL: listen (%s, %s) failed - %s", networkType, listenAddr, err)
	}
	log.Printf("HTTP: listening on %s", listenAddr)

	server := &http.Server{Handler: s.Handler}
	err = server.Serve(listener)
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		log.Printf("ERROR: http.Serve() - %s", err)
	}

	log.Printf("HTTP: closing %s", listener.Addr())
}

func (s *Server) ServeHTTPS() {
	addr := s.Opts.HttpsAddress

	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS12,
	}
	if config.NextProtos == nil {
		config.NextProtos = []string{"http/1.1"}
	}

	if s.Opts.LetsEncryptEnabled {
		manager := autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			Cache:      autocert.DirCache(s.Opts.LetsEncryptCacheDir),
			HostPolicy: autocert.HostWhitelist(s.Opts.LetsEncryptHosts...),
		}
		if s.Opts.LetsEncryptAdminEmail != "" {
			manager.Email = s.Opts.LetsEncryptAdminEmail
		}
		config.GetCertificate = manager.GetCertificate
	} else {
		var err error
		config.Certificates = make([]tls.Certificate, 1)
		config.Certificates[0], err = tls.LoadX509KeyPair(s.Opts.TLSCertFile, s.Opts.TLSKeyFile)
		if err != nil {
			log.Fatalf("FATAL: loading tls config (%s, %s) failed - %s", s.Opts.TLSCertFile, s.Opts.TLSKeyFile, err)
		}
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("FATAL: listen (%s) failed - %s", addr, err)
	}
	log.Printf("HTTPS: listening on %s", ln.Addr())

	tlsListener := tls.NewListener(tcpKeepAliveListener{ln.(*net.TCPListener)}, config)
	srv := &http.Server{Handler: s.Handler}
	err = srv.Serve(tlsListener)

	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		log.Printf("ERROR: https.Serve() - %s", err)
	}

	log.Printf("HTTPS: closing %s", tlsListener.Addr())
}

func (s *Server) ServeHTTPSRedirector() {
	h := LoggingHandler(os.Stdout, NewRedirectHandler(*s.Opts), s.Opts.RequestLogging)
	log.Printf("HTTPs redirector listening on: %s", s.Opts.HttpsRedirectorAddress)
	log.Fatal(http.ListenAndServe(s.Opts.HttpsRedirectorAddress, h))
}

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used by ListenAndServe and ListenAndServeTLS so
// dead TCP connections (e.g. closing laptop mid-download) eventually
// go away.
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}
