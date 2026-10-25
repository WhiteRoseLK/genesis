// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func TestDefaultRootTTLYears(t *testing.T) {
	if defaultRootTTLYears < 10 {
		t.Errorf("defaultRootTTLYears = %d, want at least 10", defaultRootTTLYears)
	}
}

func TestCertExpiresWithin(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// 1. Expired cert (expired 1 hour ago)
	templateExpired := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "expired.internal"},
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     time.Now().Add(-1 * time.Hour),
	}
	derExpired, err := x509.CreateCertificate(rand.Reader, templateExpired, templateExpired, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	pemExpired := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derExpired}))

	expired, err := certExpiresWithin(pemExpired, 24*time.Hour)
	if err != nil {
		t.Fatalf("certExpiresWithin: %v", err)
	}
	if !expired {
		t.Errorf("expected expired certificate to report expired=true")
	}

	// 2. Valid long-lived cert (valid for 30 years)
	templateValid := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "valid.internal"},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(30 * 365 * 24 * time.Hour),
	}
	derValid, err := x509.CreateCertificate(rand.Reader, templateValid, templateValid, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	pemValid := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derValid}))

	expired, err = certExpiresWithin(pemValid, 24*time.Hour)
	if err != nil {
		t.Fatalf("certExpiresWithin: %v", err)
	}
	if expired {
		t.Errorf("expected valid 30-year certificate to report expired=false")
	}
}
