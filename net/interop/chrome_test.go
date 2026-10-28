package interop

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
)

// chromePage joins the listener as the game does: two data channels, an
// offer posted over HTTP. Every message it receives on the reliable channel
// goes back in segments of 10000 bytes, counted down in their first byte.
const chromePage = `<!doctype html>
<script>
const report = r => fetch('/result', {method: 'POST', body: JSON.stringify(r)});
function sendSegmented(ch, msg, size) {
	const n = Math.max(1, Math.ceil(msg.length / size));
	for (let i = 0; i < n; i++) {
		const part = msg.subarray(i * size, Math.min(msg.length, (i + 1) * size));
		const seg = new Uint8Array(1 + part.length);
		seg[0] = n - 1 - i;
		seg.set(part, 1);
		ch.send(seg);
	}
}
(async () => {
	try {
		const pc = new RTCPeerConnection();
		const reliable = pc.createDataChannel('ReliableDataChannel', {ordered: true});
		pc.createDataChannel('UnreliableDataChannel', {ordered: false, maxRetransmits: 0});
		reliable.binaryType = 'arraybuffer';
		let parts = [];
		reliable.onmessage = e => {
			const b = new Uint8Array(e.data);
			parts.push(b.subarray(1));
			if (b[0] !== 0) return;
			const msg = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
			let off = 0;
			for (const p of parts) { msg.set(p, off); off += p.length; }
			parts = [];
			sendSegmented(reliable, msg, 10000);
		};
		reliable.onclose = () => report({event: 'closed'});
		await pc.setLocalDescription(await pc.createOffer());
		const resp = await fetch('/offer', {method: 'POST', body: pc.localDescription.sdp});
		const answer = await resp.text();
		if (!resp.ok) throw new Error('signaling: ' + resp.status + ' ' + answer);
		await pc.setRemoteDescription({type: 'answer', sdp: answer});
		reliable.onopen = () => report({event: 'open', maxMessageSize: pc.sctp ? pc.sctp.maxMessageSize : 0});
	} catch (e) {
		report({event: 'error', error: String(e)});
	}
})();
</script>`

func chromeBinary() string {
	if bin := os.Getenv("CHROME_BIN"); bin != "" {
		return bin
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser"} {
		if bin, err := exec.LookPath(name); err == nil {
			return bin
		}
	}
	return ""
}

// TestChromeJoinsOurListener connects headless Chrome to our listener:
// libwebrtc in the roles of the game client, the controlling ICE agent, the
// DTLS server of BoringSSL and dcSCTP opening the channels.
func TestChromeJoinsOurListener(t *testing.T) {
	bin := chromeBinary()
	if bin == "" {
		t.Skip("no Chrome found; set CHROME_BIN")
	}
	l, err := nethernet.ListenConfig{AllowAnonymous: true}.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	events := make(chan map[string]any, 8)
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, chromePage)
		case "/offer":
			// The page posts to its own origin; the listener gets the offer
			// as a client would send it.
			resp, err := http.Post("http://"+l.Addr().String()+"/v1/join/1", "application/sdp", r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			w.WriteHeader(resp.StatusCode)
			_, _ = io.Copy(w, resp.Body)
		case "/result":
			var ev map[string]any
			if json.NewDecoder(r.Body).Decode(&ev) == nil {
				events <- ev
			}
		}
	}))
	defer page.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--headless=new", "--no-sandbox", "--disable-gpu", "--no-first-run",
		"--no-default-browser-check", "--allow-loopback-in-peer-connection", "--user-data-dir="+t.TempDir(), page.URL)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Chrome runs helpers in its process group.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	}()

	accepted := make(chan *nethernet.Conn, 1)
	go func() {
		if c, err := l.AcceptConn(); err == nil {
			accepted <- c
		}
	}()
	var srv *nethernet.Conn
	for srv == nil {
		select {
		case c := <-accepted:
			srv = c
		case ev := <-events:
			if ev["event"] == "error" {
				t.Fatalf("chrome: %v", ev["error"])
			}
			t.Logf("chrome: %v", ev)
		case <-ctx.Done():
			t.Fatalf("chrome did not connect: %s", tail(stderr.String()))
		}
	}
	defer srv.Close()

	for i, size := range []int{1, 1000, 10000, 100000, 262143, 262144, 600000} {
		want := netherMessage(i, size)
		if err := srv.Send(want); err != nil {
			t.Fatal(err)
		}
		_ = srv.SetReadDeadline(time.Now().Add(15 * time.Second))
		got, err := srv.ReadMessage()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("echo of %d bytes: got %d bytes, %v", size, len(got), err)
		}
	}
	_ = srv.Close()
	for {
		select {
		case ev := <-events:
			if ev["event"] == "closed" {
				return
			}
			t.Logf("chrome: %v", ev)
		case <-time.After(15 * time.Second):
			t.Fatal("chrome did not see the channel close")
		}
	}
}

func tail(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.Join(lines[max(0, len(lines)-15):], "\n")
}
