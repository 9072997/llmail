//go:build integration

package imap

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
)

const stalwartVersion = "v0.15.5"

func stalwartAssetName() string {
	switch runtime.GOOS {
	case "windows":
		return "stalwart-x86_64-pc-windows-msvc.zip"
	case "linux":
		return "stalwart-x86_64-unknown-linux-gnu.tar.gz"
	default:
		panic("unsupported OS: " + runtime.GOOS)
	}
}

func stalwartBinaryName() string {
	if runtime.GOOS == "windows" {
		return "stalwart.exe"
	}
	return "stalwart"
}

type stalwartHarness struct {
	binPath   string
	dataDir   string
	cmd       *exec.Cmd
	imapPort  int
	httpPort  int
	adminPass string
}

var harness *stalwartHarness

func TestMain(m *testing.M) {
	h := &stalwartHarness{}
	var code int
	defer func() {
		h.cleanup()
		os.Exit(code)
	}()

	if err := h.setup(); err != nil {
		fmt.Fprintf(os.Stderr, "stalwart harness setup failed: %v\n", err)
		code = 1
		return
	}

	harness = h
	code = m.Run()
}

func (h *stalwartHarness) setup() error {
	binPath, err := ensureBinary()
	if err != nil {
		return fmt.Errorf("ensuring binary: %w", err)
	}
	h.binPath = binPath

	imapPort, err := findFreePort()
	if err != nil {
		return fmt.Errorf("finding IMAP port: %w", err)
	}
	h.imapPort = imapPort

	httpPort, err := findFreePort()
	if err != nil {
		return fmt.Errorf("finding HTTP port: %w", err)
	}
	h.httpPort = httpPort

	dataDir, err := os.MkdirTemp("", "stalwart-test-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	h.dataDir = dataDir

	adminPass, err := h.initStalwart()
	if err != nil {
		return fmt.Errorf("initializing stalwart: %w", err)
	}
	h.adminPass = adminPass

	if err := h.writeConfig(); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	if err := h.start(); err != nil {
		return fmt.Errorf("starting stalwart: %w", err)
	}

	if err := h.waitForReady(30 * time.Second); err != nil {
		return fmt.Errorf("waiting for stalwart: %w", err)
	}

	if err := h.createAccount(); err != nil {
		return fmt.Errorf("creating test account: %w", err)
	}

	return nil
}

func (h *stalwartHarness) cleanup() {
	if h.cmd != nil && h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
		_ = h.cmd.Wait()
	}
	if h.dataDir != "" {
		_ = os.RemoveAll(h.dataDir)
	}
}

func ensureBinary() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("getting cache dir: %w", err)
	}

	dir := filepath.Join(cacheDir, "llmail-test", "stalwart-"+stalwartVersion)
	binPath := filepath.Join(dir, stalwartBinaryName())

	if _, err := os.Stat(binPath); err == nil {
		return binPath, nil
	}

	fmt.Printf("Downloading Stalwart %s...\n", stalwartVersion)

	asset := stalwartAssetName()
	url := fmt.Sprintf("https://github.com/stalwartlabs/stalwart/releases/download/%s/%s", stalwartVersion, asset)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading download: %w", err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	if strings.HasSuffix(asset, ".zip") {
		if err := extractZip(body, dir); err != nil {
			return "", fmt.Errorf("extracting zip: %w", err)
		}
	} else {
		if err := extractTarGz(body, dir); err != nil {
			return "", fmt.Errorf("extracting tar.gz: %w", err)
		}
	}

	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("binary not found after extraction at %s", binPath)
	}

	fmt.Printf("Stalwart binary cached at %s\n", binPath)
	return binPath, nil
}

func extractZip(data []byte, destDir string) error {
	r, err := zip.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return err
	}

	binaryName := stalwartBinaryName()
	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if name != binaryName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		outPath := filepath.Join(destDir, binaryName)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err := io.Copy(out, rc); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("%s not found in zip archive", binaryName)
}

func extractTarGz(data []byte, destDir string) error {
	gz, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	binaryName := stalwartBinaryName()

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		name := filepath.Base(hdr.Name)
		if name != binaryName {
			continue
		}

		outPath := filepath.Join(destDir, binaryName)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		defer out.Close()

		if _, err := io.Copy(out, tr); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("%s not found in tar.gz archive", binaryName)
}

func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port, nil
}

func (h *stalwartHarness) initStalwart() (string, error) {
	cmd := exec.Command(h.binPath, "--init", h.dataDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("stalwart --init: %w\noutput: %s", err, out)
	}

	re := regexp.MustCompile(`(?i)password\s+.(\S+).`)
	matches := re.FindSubmatch(out)
	if len(matches) < 2 {
		return "", fmt.Errorf("could not parse admin password from init output:\n%s", out)
	}

	pass := strings.Trim(string(matches[1]), `'"`)
	return pass, nil
}

// generateSelfSignedCert creates a self-signed TLS certificate for testing.
func generateSelfSignedCert(dir string) (certPEM, keyPEM string, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}

	certBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	keyBlock := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})

	return string(certBlock), string(keyBlock), nil
}

func (h *stalwartHarness) writeConfig() error {
	configPath := filepath.Join(h.dataDir, "etc", "config.toml")
	dataPath := filepath.ToSlash(h.dataDir)

	// Read the init-generated config to extract the hashed admin password
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading generated config: %w", err)
	}

	secretRe := regexp.MustCompile(`(?m)^secret\s*=\s*"(.+)"`)
	matches := secretRe.FindSubmatch(data)
	hashedSecret := h.adminPass
	if len(matches) >= 2 {
		hashedSecret = string(matches[1])
	}

	// Generate self-signed TLS cert (Stalwart disables LOGIN on cleartext)
	certPEM, keyPEM, err := generateSelfSignedCert(filepath.Join(h.dataDir, "etc"))
	if err != nil {
		return fmt.Errorf("generating TLS cert: %w", err)
	}

	config := fmt.Sprintf(`[server.listener.imaptls]
bind = ["127.0.0.1:%d"]
protocol = "imap"
tls.implicit = true

[server.listener.http]
bind = ["127.0.0.1:%d"]
protocol = "http"
tls.implicit = false

[certificate.default]
cert = '''%s'''
private-key = '''%s'''

[store.rocksdb]
type = "rocksdb"
path = "%s/data"
compression = "lz4"

[directory.internal]
type = "internal"
store = "rocksdb"

[storage]
data = "rocksdb"
blob = "rocksdb"
fts = "rocksdb"
lookup = "rocksdb"
directory = "internal"

[authentication.fallback-admin]
user = "admin"
secret = "%s"

[tracer.stdout]
type = "stdout"
level = "warn"
`, h.imapPort, h.httpPort, certPEM, keyPEM, dataPath, hashedSecret)

	return os.WriteFile(configPath, []byte(config), 0o644)
}

func (h *stalwartHarness) start() error {
	configPath := filepath.Join(h.dataDir, "etc", "config.toml")
	h.cmd = exec.Command(h.binPath, "--config", configPath)
	h.cmd.Stdout = os.Stdout
	h.cmd.Stderr = os.Stderr
	return h.cmd.Start()
}

func (h *stalwartHarness) waitForReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ports := []int{h.imapPort, h.httpPort}

	for _, port := range ports {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
			if err == nil {
				conn.Close()
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		conn, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err != nil {
			return fmt.Errorf("port %d not ready after %v", port, timeout)
		}
		conn.Close()
	}
	return nil
}

func (h *stalwartHarness) createAccount() error {
	if err := h.apiCreatePrincipal(map[string]interface{}{
		"type": "domain",
		"name": "localhost",
	}); err != nil {
		return fmt.Errorf("creating domain: %w", err)
	}

	if err := h.apiCreatePrincipal(map[string]interface{}{
		"type":    "individual",
		"name":    "testuser",
		"secrets": []string{"testpass123"},
		"emails":  []string{"testuser@localhost"},
		"roles":   []string{"user"},
	}); err != nil {
		return fmt.Errorf("creating user: %w", err)
	}

	return nil
}

func (h *stalwartHarness) apiCreatePrincipal(payload map[string]interface{}) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/api/principal", h.httpPort)
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", h.adminPass)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, respBody)
	}
	return nil
}

// --- Test Helpers ---

func dialTestClient(t *testing.T) *imapclient.Client {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", harness.imapPort)
	c, err := imapclient.DialTLS(addr, &imapclient.Options{
		TLSConfig: &tls.Config{InsecureSkipVerify: true},
	})
	if err != nil {
		t.Fatalf("dialing IMAP: %v", err)
	}
	if err := c.Authenticate(sasl.NewPlainClient("", "testuser", "testpass123")); err != nil {
		c.Close()
		t.Fatalf("authenticate: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func setupTestFolder(t *testing.T, c *imapclient.Client) string {
	t.Helper()
	return setupTestFolderNamed(t, c, "Test_"+strings.ReplaceAll(t.Name(), "/", "_"))
}

func setupTestFolderNamed(t *testing.T, c *imapclient.Client, folder string) string {
	t.Helper()
	if err := c.Create(folder, nil).Wait(); err != nil {
		t.Fatalf("creating folder %q: %v", folder, err)
	}
	t.Cleanup(func() {
		_ = c.Delete(folder).Wait()
	})
	return folder
}

func seedMessages(t *testing.T, c *imapclient.Client, folder string, count int) []uint32 {
	t.Helper()
	ctx := context.Background()
	var uids []uint32
	for i := 1; i <= count; i++ {
		res, err := AppendMessage(ctx, c, AppendParams{
			Folder:  folder,
			From:    "testuser@localhost",
			To:      []string{"recipient@localhost"},
			Subject: fmt.Sprintf("Test Message %d", i),
			Body:    fmt.Sprintf("Body of test message %d", i),
		})
		if err != nil {
			t.Fatalf("seeding message %d: %v", i, err)
		}
		uids = append(uids, res.UID)
	}
	return uids
}

func seedThread(t *testing.T, c *imapclient.Client, folder string, count int) []uint32 {
	t.Helper()
	var uids []uint32
	var messageIDs []string

	for i := 0; i < count; i++ {
		msgID := fmt.Sprintf("<thread-%s-%d@localhost>", t.Name(), i)
		messageIDs = append(messageIDs, msgID)

		var references string
		var inReplyTo string
		if i > 0 {
			inReplyTo = messageIDs[i-1]
			references = strings.Join(messageIDs[:i], " ")
		}

		date := time.Date(2025, 1, 1, 10, i, 0, 0, time.UTC)
		msg := buildRawMessage(msgID, inReplyTo, references,
			fmt.Sprintf("Thread Message %d", i),
			"testuser@localhost", "recipient@localhost",
			fmt.Sprintf("Thread body %d", i),
			date)

		appendCmd := c.Append(folder, int64(len(msg)), nil)
		if _, err := io.Copy(appendCmd, strings.NewReader(msg)); err != nil {
			t.Fatalf("writing thread message %d: %v", i, err)
		}
		if err := appendCmd.Close(); err != nil {
			t.Fatalf("closing append for thread message %d: %v", i, err)
		}
		data, err := appendCmd.Wait()
		if err != nil {
			t.Fatalf("appending thread message %d: %v", i, err)
		}
		uids = append(uids, uint32(data.UID))
	}
	return uids
}

func buildRawMessage(messageID, inReplyTo, references, subject, from, to, body string, date time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Message-ID: %s\r\n", messageID)
	fmt.Fprintf(&b, "Date: %s\r\n", date.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	if inReplyTo != "" {
		fmt.Fprintf(&b, "In-Reply-To: %s\r\n", inReplyTo)
	}
	if references != "" {
		fmt.Fprintf(&b, "References: %s\r\n", references)
	}
	fmt.Fprintf(&b, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: text/plain; charset=UTF-8\r\n")
	fmt.Fprintf(&b, "\r\n")
	fmt.Fprintf(&b, "%s\r\n", body)
	return b.String()
}

func seedMessageWithAttachment(t *testing.T, c *imapclient.Client, folder string) uint32 {
	t.Helper()
	boundary := "----=_Part_12345"
	date := time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05 -0700")

	msg := fmt.Sprintf("From: testuser@localhost\r\n"+
		"To: recipient@localhost\r\n"+
		"Subject: Message With Attachment\r\n"+
		"Date: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: multipart/mixed; boundary=\"%s\"\r\n"+
		"\r\n"+
		"--%s\r\n"+
		"Content-Type: text/plain; charset=UTF-8\r\n"+
		"\r\n"+
		"This message has an attachment.\r\n"+
		"--%s\r\n"+
		"Content-Type: application/octet-stream; name=\"test.bin\"\r\n"+
		"Content-Disposition: attachment; filename=\"test.bin\"\r\n"+
		"Content-Transfer-Encoding: base64\r\n"+
		"\r\n"+
		"SGVsbG8gV29ybGQh\r\n"+
		"--%s--\r\n",
		date, boundary, boundary, boundary, boundary)

	appendCmd := c.Append(folder, int64(len(msg)), nil)
	if _, err := io.Copy(appendCmd, strings.NewReader(msg)); err != nil {
		t.Fatalf("writing attachment message: %v", err)
	}
	if err := appendCmd.Close(); err != nil {
		t.Fatalf("closing append for attachment message: %v", err)
	}
	data, err := appendCmd.Wait()
	if err != nil {
		t.Fatalf("appending attachment message: %v", err)
	}
	return uint32(data.UID)
}

func seedHTMLMessage(t *testing.T, c *imapclient.Client, folder string) uint32 {
	t.Helper()
	ctx := context.Background()
	res, err := AppendMessage(ctx, c, AppendParams{
		Folder:   folder,
		From:     "testuser@localhost",
		To:       []string{"recipient@localhost"},
		Subject:  "HTML Test Message",
		Body:     "Plain text version",
		HTMLBody: "<h1>HTML version</h1>",
	})
	if err != nil {
		t.Fatalf("seeding HTML message: %v", err)
	}
	return res.UID
}

// containsFlag checks if a flag is present in a flag list.
func containsFlag(flags []string, target string) bool {
	for _, f := range flags {
		if f == target {
			return true
		}
	}
	return false
}

// uidInFolder checks if a UID exists in a folder by searching for it.
func uidInFolder(t *testing.T, c *imapclient.Client, folder string, uid uint32) bool {
	t.Helper()
	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		t.Fatalf("selecting folder %q: %v", folder, err)
	}

	uidSet := imap.UIDSet{}
	uidSet.AddNum(imap.UID(uid))

	msgs, err := c.Fetch(uidSet, &imap.FetchOptions{UID: true}).Collect()
	if err != nil {
		return false
	}
	return len(msgs) > 0
}
