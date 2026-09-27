// SPDX-License-Identifier: Apache-2.0

//go:build docker

package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/runner"
	"github.com/WhiteRoseLK/genesis/internal/secrets"
	"github.com/WhiteRoseLK/genesis/internal/testutil"
	pkiissuerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/pki/issuer/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// launchStepCA lance step-ca avec une session de broker vers core.container/v1
// (Docker réel) et core.secrets/v1 (réel, age+fichier) — même mécanisme que
// modules/coredns, reproduit ici pour tester le module isolément.
func launchStepCA(t *testing.T) pkiissuerv1.PkiIssuerClient {
	t.Helper()
	rt := testutil.RequireRuntime(t)
	return launchStepCAWithStore(t, rt, newTestSecretsStore(t))
}

// launchStepCAWithStore permet de relancer step-ca (nouveau process, comme
// un redémarrage du cœur) sur le MÊME store de secrets, pour prouver
// l'idempotence de la CA à travers deux exécutions distinctes.
func launchStepCAWithStore(t *testing.T, rt *runner.ContainerRuntime, store *secrets.FileStore) pkiissuerv1.PkiIssuerClient {
	t.Helper()
	ctx := context.Background()

	registry := broker.NewRegistry()
	registry.SetNative("core.container/v1", broker.NativeContainer(rt))
	registry.SetNative("core.secrets/v1", broker.NativeSecrets(store))

	binaryPath, manifest := buildModule(t, "step-ca")
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch : %v", err)
	}
	t.Cleanup(client.Close)

	token := registry.OpenSession(client.Broker(), "step-ca", []string{"core.container/v1", "core.secrets/v1"})
	checkResp, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Check : %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_A_FAIRE {
		t.Fatalf("Check().Status = %v, attendu A_FAIRE (pas encore amorcé)", checkResp.GetStatus())
	}

	seedToken := registry.OpenSession(client.Broker(), "step-ca", []string{"core.container/v1", "core.secrets/v1"})
	seedResp, err := client.Module().SeedUp(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: seedToken})
	if err != nil {
		t.Fatalf("SeedUp : %v", err)
	}
	if seedResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("SeedUp().Status = %v", seedResp.GetStatus())
	}

	conn, err := client.DispenseFunction("pki.issuer/v1")
	if err != nil {
		t.Fatalf("DispenseFunction(pki.issuer/v1) : %v", err)
	}
	return pkiissuerv1.NewPkiIssuerClient(conn)
}

func parseCertPEM(t *testing.T, certPEM string) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		t.Fatalf("PEM illisible : %q", certPEM)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("certificat illisible : %v", err)
	}
	return cert
}

func verifyAgainstRoot(t *testing.T, chainPEM, rootPEM string) {
	t.Helper()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(rootPEM)) {
		t.Fatal("racine illisible")
	}
	intermediates := x509.NewCertPool()
	var leaf *x509.Certificate
	rest := []byte(chainPEM)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("certificat illisible dans la chaîne : %v", err)
		}
		if leaf == nil {
			leaf = cert
			continue
		}
		intermediates.AddCert(cert)
	}
	if leaf == nil {
		t.Fatal("aucun certificat feuille dans la chaîne")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		t.Fatalf("chaîne invalide jusqu'à la racine : %v", err)
	}
}

// TestStepCAIssuesAndSignsRealCertificates prouve, contre le vrai
// conteneur smallstep/step-ca (docs/08-milestones.md, J7), que IssueCert et
// SignCSR produisent des certificats réellement valides jusqu'à la racine
// générée par la CA, que CAChain expose cette racine, et que SignSSH reste
// un stub explicite (pas encore de consommateur, différé à J8).
func TestStepCAIssuesAndSignsRealCertificates(t *testing.T) {
	pki := launchStepCA(t)
	ctx := context.Background()

	chain, err := pki.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		t.Fatalf("CAChain : %v", err)
	}
	if chain.GetChainPem() == "" {
		t.Fatal("CAChain : chaîne vide")
	}
	rootCert := parseCertPEM(t, chain.GetChainPem())
	if !rootCert.IsCA {
		t.Error("CAChain : le dernier certificat de la chaîne n'est pas une CA")
	}

	issued, err := pki.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{
		CommonName: "infra01.lab.internal",
		Sans:       []string{"infra01.lab.internal"},
		TtlSeconds: 3600,
	})
	if err != nil {
		t.Fatalf("IssueCert : %v", err)
	}
	if issued.GetPrivateKeyPem() == "" {
		t.Error("IssueCert : private_key_pem vide, attendu une clé générée par le fournisseur")
	}
	leaf := parseCertPEM(t, issued.GetCertPem())
	if leaf.Subject.CommonName != "infra01.lab.internal" {
		t.Errorf("IssueCert : CN = %q, attendu infra01.lab.internal", leaf.Subject.CommonName)
	}
	verifyAgainstRoot(t, issued.GetChainPem(), chain.GetChainPem())

	// SignCSR : un vrai CSR généré ici (la clé privée ne quitte jamais ce
	// process, contrairement à IssueCert).
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("génération de la clé du CSR : %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "test.lab.internal"},
	}, key)
	if err != nil {
		t.Fatalf("création du CSR : %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	signed, err := pki.SignCSR(ctx, &pkiissuerv1.SignCSRRequest{CsrPem: string(csrPEM), TtlSeconds: 3600})
	if err != nil {
		t.Fatalf("SignCSR : %v", err)
	}
	if signed.GetPrivateKeyPem() != "" {
		t.Error("SignCSR : private_key_pem devrait rester vide, la clé appartient à l'appelant")
	}
	signedLeaf := parseCertPEM(t, signed.GetCertPem())
	if signedLeaf.Subject.CommonName != "test.lab.internal" {
		t.Errorf("SignCSR : CN = %q, attendu test.lab.internal", signedLeaf.Subject.CommonName)
	}
	verifyAgainstRoot(t, signed.GetChainPem(), chain.GetChainPem())

	if _, err := pki.SignSSH(ctx, &pkiissuerv1.SignSSHRequest{KeyId: "test"}); err == nil {
		t.Error("SignSSH : succès inattendu, devrait encore être Unimplemented (différé à J8)")
	}
}

// TestStepCAReusesRootAcrossRestarts prouve l'idempotence de la CA
// (docs/03-module-contract.md §4 règle 3) à travers un vrai redémarrage du
// module (nouveau process, pas juste un cache en mémoire) : la seconde
// instance doit retrouver la MÊME racine, stockée par la première via
// core.secrets, pas en générer une nouvelle.
func TestStepCAReusesRootAcrossRestarts(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	store := newTestSecretsStore(t)
	ctx := context.Background()

	first := launchStepCAWithStore(t, rt, store)
	firstChain, err := first.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		t.Fatalf("CAChain (première instance) : %v", err)
	}

	second := launchStepCAWithStore(t, rt, store)
	secondChain, err := second.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		t.Fatalf("CAChain (seconde instance) : %v", err)
	}

	if firstChain.GetChainPem() != secondChain.GetChainPem() {
		t.Error("la seconde instance de step-ca a régénéré une chaîne différente au lieu de réutiliser celle déjà stockée")
	}
}

// TestStepCASignsThirdPartyIntermediate prouve le scénario exact dont
// modules/vault (J7) aura besoin : SignCSR avec is_ca=true doit produire un
// VRAI intermédiaire, capable de signer à son tour une feuille, avec une
// chaîne complète (feuille tierce -> intermédiaire tiers -> intermédiaire
// step-ca -> racine) valide jusqu'à la racine de step-ca
// (docs/07-mvp-modules.md : "pki_int signé par la racine"). Ce chemin a
// révélé un vrai bug (pathlen insuffisant sur la racine par défaut de
// step-ca) découvert en validant manuellement le workflow Vault dans
// Docker avant d'écrire modules/vault.
func TestStepCASignsThirdPartyIntermediate(t *testing.T) {
	pki := launchStepCA(t)
	ctx := context.Background()

	rootChain, err := pki.CAChain(ctx, &pkiissuerv1.Empty{})
	if err != nil {
		t.Fatalf("CAChain : %v", err)
	}

	// CSR d'un intermédiaire tiers (comme le ferait modules/vault pour
	// pki_int/intermediate/generate/internal).
	intKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("génération de la clé de l'intermédiaire tiers : %v", err)
	}
	intCSRDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "Third Party Intermediate"},
	}, intKey)
	if err != nil {
		t.Fatalf("création du CSR de l'intermédiaire tiers : %v", err)
	}
	intCSRPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: intCSRDER})

	signedInt, err := pki.SignCSR(ctx, &pkiissuerv1.SignCSRRequest{
		CsrPem: string(intCSRPEM), IsCa: true, PathLenConstraint: 0, TtlSeconds: 3600,
	})
	if err != nil {
		t.Fatalf("SignCSR (is_ca=true) : %v", err)
	}
	intCert := parseCertPEM(t, signedInt.GetCertPem())
	if !intCert.IsCA {
		t.Fatal("SignCSR (is_ca=true) : le certificat obtenu n'est pas une CA")
	}
	verifyAgainstRoot(t, signedInt.GetChainPem(), rootChain.GetChainPem())

	// L'intermédiaire tiers signe maintenant sa propre feuille, en Go pur —
	// prouve que le certificat émis est un VRAI intermédiaire fonctionnel,
	// pas seulement marqué CA:TRUE.
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("génération de la clé de la feuille : %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "leaf-under-third-party.lab.internal"},
		NotBefore:    intCert.NotBefore,
		NotAfter:     intCert.NotAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, intCert, &leafKey.PublicKey, intKey)
	if err != nil {
		t.Fatalf("signature de la feuille par l'intermédiaire tiers : %v", err)
	}
	leafPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})

	fullChain := string(leafPEM) + signedInt.GetChainPem()
	verifyAgainstRoot(t, fullChain, rootChain.GetChainPem())
}
