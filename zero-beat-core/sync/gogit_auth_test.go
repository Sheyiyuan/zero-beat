package sync

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestGoGitTokenAndSSHHostVerification(t *testing.T) {
	web := &goGit{remote: "https://example.test/repo.git", token: "test-token"}
	auth, err := web.auth()
	if err != nil {
		t.Fatal(err)
	}
	if a, ok := auth.(*githttp.BasicAuth); !ok || a.Password != "test-token" {
		t.Fatal("HTTPS token not passed to transport")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.MarshalPrivateKey(private, "test")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	keyPath, hostsPath := filepath.Join(dir, "key"), filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(key), 0o600); err != nil {
		t.Fatal(err)
	}
	hosts := knownhosts.Line([]string{"git.example.test"}, signer.PublicKey()) + "\n"
	if err := os.WriteFile(hostsPath, []byte(hosts), 0o600); err != nil {
		t.Fatal(err)
	}
	g := &goGit{remote: "ssh://git@git.example.test/repo.git", sshKey: keyPath, knownHosts: hostsPath}
	auth, err = g.auth()
	if err != nil {
		t.Fatal(err)
	}
	a, ok := auth.(*gitssh.PublicKeys)
	if !ok {
		t.Fatal("explicit SSH key ignored")
	}
	address := &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 22}
	if err := a.HostKeyCallback("git.example.test:22", address, signer.PublicKey()); err != nil {
		t.Fatal("trusted host rejected", err)
	}
	if err := a.HostKeyCallback("unknown.example.test:22", address, signer.PublicKey()); err == nil {
		t.Fatal("unknown host accepted")
	}
	_, other, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherSigner, err := ssh.NewSignerFromKey(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.HostKeyCallback("git.example.test:22", address, otherSigner.PublicKey()); err == nil {
		t.Fatal("changed host key accepted")
	}
}
