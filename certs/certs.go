// Package certs creates the PKCS#12 keystores and self-signed identities modules mount.
package certs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// WriteKeystore writes a PKCS#12 keystore holding a fresh RSA-2048 key and a self-signed
// certificate valid for ten years. It refuses to overwrite an existing file: a keystore
// that signed data must not be replaced silently.
func WriteKeystore(path, password, commonName string) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return false, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return false, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName, Organization: []string{"tsi-cli generated"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return false, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return false, err
	}
	data, err := pkcs12.Modern.Encode(key, cert, nil, password)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	// 0644 inside a 0700 directory: the container may run as a different user and
	// must be able to read the keystore; the directory keeps other host users out.
	return true, os.WriteFile(path, data, 0o644)
}

// Identity is a node's key and self-signed certificate in PEM form.
type Identity struct {
	CertificatePEM string
	PrivateKeyPEM  string // PKCS#8
}

// LoadOrCreateIdentity returns the node identity stored as <dir>/<name>.pem and
// <dir>/<name>.key, creating it (RSA-2048, ten years, SAN = host) if absent. An existing
// identity is never replaced: peers have pinned its public key.
func LoadOrCreateIdentity(dir, name, host string) (*Identity, bool, error) {
	certPath, keyPath := filepath.Join(dir, name+".pem"), filepath.Join(dir, name+".key")
	cb, cerr := os.ReadFile(certPath)
	kb, kerr := os.ReadFile(keyPath)
	if cerr == nil && kerr == nil {
		return &Identity{CertificatePEM: string(cb), PrivateKeyPEM: string(kb)}, false, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, false, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, false, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host, Organization: []string{"tsi-cli generated"}},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, false, err
	}
	pk8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, false, err
	}
	id := &Identity{
		CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		PrivateKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk8})),
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(keyPath, []byte(id.PrivateKeyPEM), 0o600); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(certPath, []byte(id.CertificatePEM), 0o644); err != nil {
		return nil, false, err
	}
	return id, true, nil
}
