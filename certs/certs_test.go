package certs

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"software.sslmate.com/src/go-pkcs12"
)

func TestWriteKeystoreIsValidAndNeverOverwritten(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "ks.p12")
	created, err := WriteKeystore(p, "a-strong-pass-123", "Test CN")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	b, _ := os.ReadFile(p)
	key, cert, err := pkcs12.Decode(b, "a-strong-pass-123")
	if err != nil || key == nil || cert.Subject.CommonName != "Test CN" {
		t.Fatalf("decode: %v %v", err, cert)
	}
	if _, _, err := pkcs12.Decode(b, "wrong"); err == nil {
		t.Fatal("the wrong password must not open the keystore")
	}
	created, err = WriteKeystore(p, "another-pass", "Other")
	if err != nil || created {
		t.Fatalf("an existing keystore must be left alone: created=%v err=%v", created, err)
	}
	b2, _ := os.ReadFile(p)
	if string(b) != string(b2) {
		t.Fatal("keystore content changed")
	}
}

func TestIdentityIsCreatedOnceAndPinned(t *testing.T) {
	dir := t.TempDir()
	a, created, err := LoadOrCreateIdentity(dir, "node-a", "node-a-server-1")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	block, _ := pem.Decode([]byte(a.CertificatePEM))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || cert.Subject.CommonName != "node-a-server-1" || len(cert.DNSNames) != 1 || cert.DNSNames[0] != "node-a-server-1" {
		t.Fatalf("certificate: %v %+v", err, cert)
	}
	kblock, _ := pem.Decode([]byte(a.PrivateKeyPEM))
	if kblock == nil || kblock.Type != "PRIVATE KEY" {
		t.Fatalf("the private key must be PKCS#8 (%v)", kblock)
	}
	if _, err := x509.ParsePKCS8PrivateKey(kblock.Bytes); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "node-a.key"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("private key mode %v, want 0600", fi.Mode().Perm())
	}
	b, created, err := LoadOrCreateIdentity(dir, "node-a", "other")
	if err != nil || created || b.CertificatePEM != a.CertificatePEM || b.PrivateKeyPEM != a.PrivateKeyPEM {
		t.Fatal("an existing identity must be returned unchanged: peers have pinned its public key")
	}
}
