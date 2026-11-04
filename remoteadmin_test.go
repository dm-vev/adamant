package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/remoteadmin"
	"github.com/df-mc/dragonfly/server/world"
)

// remoteTestServer returns a running server of no saved data whose lists
// are in dir.
func remoteTestServer(t *testing.T, dir string) *server.Server {
	t.Helper()
	uc := server.DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.World.DisableNether, uc.World.DisableEnd = true, true
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	conf.Log = slog.New(slog.DiscardHandler)
	conf.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	srv := conf.New()
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// writeTestCertificate writes a certificate of 127.0.0.1 and its key to
// cert.pem and key.pem in dir, and returns a pool that trusts it.
func writeTestCertificate(t *testing.T, dir string) *x509.CertPool {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for name, block := range map[string]*pem.Block{"cert.pem": {Type: "CERTIFICATE", Bytes: der}, "key.pem": {Type: "EC PRIVATE KEY", Bytes: keyDER}} {
		if err := os.WriteFile(filepath.Join(dir, name), pem.EncodeToMemory(block), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return pool
}

// ownerToken returns the token of owner the file of tokens of dir holds.
func ownerToken(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Fields(line); len(f) == 3 && f[0] == "owner" {
			return f[2]
		}
	}
	t.Fatalf("no owner token in %s", path)
	return ""
}

// status asks GET /v1/status of the client passed, at the URL passed, with
// the token passed, and returns the status code.
func status(t *testing.T, c *http.Client, url, token string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url+"/v1/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// TestRemoteListenerConfig checks the listener of [RemoteAdmin]: off by
// default; on loopback without TLS, it makes the file of tokens, mode
// 0600, logs that it did without the token, and serves; with a
// certificate, it serves TLS 1.2 or newer, and refuses TLS 1.1. Off
// loopback without TLS, the file is refused (TestConfigFileErrors).
func TestRemoteListenerConfig(t *testing.T) {
	dir := t.TempDir()
	srv := remoteTestServer(t, dir)
	if l, err := defaultFileConfig().RemoteAdmin.Listen(srv, remoteadmin.ListenOptions{Dir: dir}); l != nil || err != nil {
		t.Fatalf("the default table serves: %v, %v", l, err)
	}

	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, nil))
	c := remoteadmin.DefaultConfig()
	c.RemoteAdmin.Enabled, c.RemoteAdmin.Address = true, "127.0.0.1:0"
	l, err := c.Listen(srv, remoteadmin.ListenOptions{Dir: dir, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	path := filepath.Join(dir, remoteadmin.DefaultTokenFile)
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("the file of tokens: %v, %v, want mode 0600", fi, err)
	}
	token := ownerToken(t, path)
	if !strings.Contains(logs.String(), "Remote admin token written to "+path+".") || strings.Contains(logs.String(), token) {
		t.Errorf("the log of the listener:\n%s\nwant the file named and the token never", logs.String())
	}
	if code := status(t, http.DefaultClient, "http://"+l.Addr().String(), token); code != http.StatusOK {
		t.Errorf("GET /v1/status over loopback: %d", code)
	}
	if code := status(t, http.DefaultClient, "http://"+l.Addr().String(), "wrong"+token); code != http.StatusUnauthorized {
		t.Errorf("GET /v1/status with a wrong token: %d", code)
	}

	pool := writeTestCertificate(t, dir)
	c.RemoteAdmin.CertFile, c.RemoteAdmin.KeyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	tl, err := c.Listen(srv, remoteadmin.ListenOptions{Dir: dir, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	defer tl.Close()
	url := "https://" + tl.Addr().String()
	secure := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	if code := status(t, secure, url, token); code != http.StatusOK {
		t.Errorf("GET /v1/status over TLS: %d", code)
	}
	old := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11}}}
	if resp, err := old.Get(url + "/v1/status"); err == nil {
		_ = resp.Body.Close()
		t.Error("a client of TLS 1.1 was served")
	}
	if resp, err := http.Get("http://" + tl.Addr().String() + "/v1/status"); err == nil {
		if resp.StatusCode == http.StatusOK {
			t.Error("plain HTTP was served on the listener of TLS")
		}
		_ = resp.Body.Close()
	}
}

// TestRemoteTokenFileMode checks that a file of tokens others may read is
// warned of as the file is read, and refused with StrictConfig, and that a
// file of mode 0600 is not.
func TestRemoteTokenFileMode(t *testing.T) {
	const table = "[RemoteAdmin]\n  Enabled = true\n  TokenFile = \"tokens.txt\"\n"
	for _, tc := range []struct {
		mode   os.FileMode
		strict bool
		want   string
	}{
		{0600, false, ""},
		{0644, false, "warning: line 3: RemoteAdmin.TokenFile tokens.txt may be read by others than its owner: chmod 600 it"},
		{0644, true, "line 5: RemoteAdmin.TokenFile tokens.txt may be read by others than its owner: chmod 600 it"},
	} {
		t.Chdir(t.TempDir())
		if err := os.WriteFile("tokens.txt", []byte("owner 4 "+strings.Repeat("a", 40)+"\n"), tc.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod("tokens.txt", tc.mode); err != nil {
			t.Fatal(err)
		}
		file := table
		if tc.strict {
			file = "[Server]\n  StrictConfig = true\n" + table
		}
		if err := os.WriteFile("config.toml", []byte(file), 0644); err != nil {
			t.Fatal(err)
		}
		f, err := readConfigFile("config.toml")
		got := ""
		switch {
		case err != nil:
			got = err.Error()
		case len(f.warnings) > 0:
			got = "warning: " + f.warnings[0].String()
		}
		if got != tc.want {
			t.Errorf("mode %v, strict %t: %q, want %q", tc.mode, tc.strict, got, tc.want)
		}
	}
}
