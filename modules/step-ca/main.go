// SPDX-License-Identifier: Apache-2.0

// step-ca fournit pki.issuer/v1 en phase graine (docs/07-mvp-modules.md) :
// CA racine (stockée en recovery via core.secrets) + intermédiaire graine,
// émission/signature de certificats en pilotant réellement le CLI `step`
// du conteneur smallstep/step-ca (docs/03-module-contract.md règle 8 :
// "orchestrer, ne pas réinventer"). Signature purement locale/hors-ligne
// (`step certificate create`/`sign` avec les fichiers de la CA montés) —
// pas de serveur step-ca détaché : évite la complexité TLS/DNS/provisioner
// du mode réseau du produit, non nécessaire à ce jalon (rien ne consomme
// encore l'API HTTP de step-ca). SignSSH reste un stub explicite
// (Unimplemented) : rien ne le consomme avant openssh-bastion (J8), où il
// sera construit et testé pour de vrai contre un consommateur réel — même
// méthode que Harden dans modules/base-os (J5).
package main

import (
	"context"
	"crypto/x509"
	_ "embed"
	"encoding/pem"
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	containerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/container/v1"
	secretsv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
	pkiissuerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/pki/issuer/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

//go:embed module.yaml
var manifestYAML []byte

const (
	stepCAImage                = "smallstep/step-ca:0.30.2@sha256:a2b17872915c193259b75a5474c398326f41bd199f0842093e52cf4182bc8270"
	caPasswordRef              = "step-ca/ca-password"
	rootCertRef                = "step-ca/root-cert"
	rootKeyRef                 = "step-ca/root-key"
	intermediateCertRef        = "step-ca/intermediate-cert"
	intermediateKeyRef         = "step-ca/intermediate-key"
	defaultIntermediateTTLDays = 30
	// expiryMargin : régénère l'intermédiaire un peu avant son expiration
	// réelle plutôt que d'attendre l'échéance exacte (dette : pas de
	// renouvellement planifié/périodique, seulement détecté au prochain
	// appel — docs/PROGRESS.md).
	expiryMargin = 24 * time.Hour
	// intermediatePathLen : profondeur d'intermédiaires supplémentaires que
	// l'intermédiaire graine de step-ca peut lui-même signer — 1, pour
	// permettre à un pki.issuer/v1 tiers (ex. vault) d'obtenir SON propre
	// intermédiaire (pathlen 0, feuilles uniquement) signé par celui-ci.
	// La racine doit donc autoriser au moins 2 niveaux (elle → cet
	// intermédiaire → l'intermédiaire du tiers), vérifié manuellement dans
	// Docker : --profile root-ca (pathlen:1 par défaut) est insuffisant.
	intermediatePathLen = 1
	// notBeforeSkew : recule légèrement le début de validité de chaque
	// certificat émis/signé, pour éviter une erreur "notBefore before
	// signer's notBefore" côté consommateur (ex. Vault) quand son horloge
	// ou son émission suit de quelques secondes la signature de son propre
	// intermédiaire — observé manuellement, pratique standard PKI.
	notBeforeSkew = "-1m"
)

// rootTemplate fixe maxPathLen sur la racine auto-signée — --template est
// le seul moyen d'y parvenir (incompatible avec --profile root-ca, qui
// impose pathlen:1).
const rootTemplate = `{
  "subject": {"commonName": "Genesis Root CA"},
  "issuer": {"commonName": "Genesis Root CA"},
  "keyUsage": ["certSign", "crlSign"],
  "basicConstraints": {"isCA": true, "maxPathLen": 2}
}`

// pkiMaterial est le matériel PKI actif, mis en cache en mémoire une fois
// chargé/généré — même dette que modules/coredns et modules/powerdns
// (docs/03 §2 : pas d'état local, mais rien ne transmet cet état aux
// gestionnaires de fonction, qui n'ont pas de StepRequest.state).
type pkiMaterial struct {
	RootCertPEM         string
	IntermediateCertPEM string
	IntermediateKeyPEM  string
	Password            string
}

type stepCAModule struct {
	modulev1.UnimplementedModuleServer
	manifest *modulev1.Manifest

	mu                  sync.Mutex
	broker              *sdk.BrokerClient
	brokerToken         string
	containerClient     containerv1.ContainerClient
	secretsClient       secretsv1.SecretsClient
	intermediateTTLDays int
	pki                 *pkiMaterial
}

func (m *stepCAModule) SetBroker(b *sdk.BrokerClient) { m.broker = b }

func (m *stepCAModule) Describe(context.Context, *modulev1.Empty) (*modulev1.Manifest, error) {
	return m.manifest, nil
}

func (m *stepCAModule) Validate(context.Context, *modulev1.ValidateRequest) (*modulev1.Diagnostics, error) {
	return &modulev1.Diagnostics{}, nil
}

func (m *stepCAModule) Check(_ context.Context, req *modulev1.StepRequest) (*modulev1.CheckResult, error) {
	m.brokerToken = req.GetBrokerToken()
	flags := sdk.StateMap(req.GetState())
	if boolFlag(flags, "seeded") || boolFlag(flags, "retired") {
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_CONFORME}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_A_FAIRE}, nil
}

// dial dial la session de broker au plus une fois (Dial ne réussit qu'une
// fois par jeton) — même précaution que modules/chrony.
func (m *stepCAModule) dial() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.containerClient != nil {
		return nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return fmt.Errorf("step-ca : aucune session de broker (Check n'a pas encore été appelé)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return fmt.Errorf("connexion aux fonctions requises : %w", err)
	}
	m.containerClient = containerv1.NewContainerClient(conn)
	m.secretsClient = secretsv1.NewSecretsClient(conn)
	return nil
}

// SeedUp amorce la CA (racine + intermédiaire) : c'est le seul jalon du
// cycle de vie d'un module graine pur (docs/03 §5), Verify doit pouvoir
// émettre un certificat juste après, donc le travail se fait ici, pas
// paresseusement au premier appel (contrairement à modules/coredns).
func (m *stepCAModule) SeedUp(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	cfg := sdk.StateMap(req.GetConfig())
	days := defaultIntermediateTTLDays
	if d, ok := cfg["intermediate_ttl_days"].(float64); ok && d > 0 {
		days = int(d)
	}
	m.mu.Lock()
	m.intermediateTTLDays = days
	m.mu.Unlock()

	if _, err := m.ensurePKI(ctx); err != nil {
		return nil, err
	}
	return m.setFlag(req, "seeded")
}

// Verify émet un certificat de test et valide sa chaîne cryptographiquement
// (docs/03 §4 règle 2) — pki.issuer/v1 est la fonction elle-même : pas
// besoin d'un tiers réseau, la vérification directe de la chaîne EST le
// test consommateur le plus direct possible ici.
func (m *stepCAModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	cert, err := m.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{
		CommonName: "verify.step-ca.internal",
		TtlSeconds: int64((24 * time.Hour).Seconds()),
	})
	if err != nil {
		return nil, fmt.Errorf("Verify(step-ca) : %w", err)
	}
	material, err := m.ensurePKI(ctx)
	if err != nil {
		return nil, err
	}
	if err := verifyChain(cert.GetChainPem(), material.RootCertPEM); err != nil {
		return nil, fmt.Errorf("Verify(step-ca) : chaîne invalide : %w", err)
	}
	return m.setFlag(req, "verified")
}

func (m *stepCAModule) SeedDown(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	return m.setFlag(req, "retired")
}

func (m *stepCAModule) Destroy(_ context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	s, err := sdk.NewState(map[string]any{})
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func (m *stepCAModule) setFlag(req *modulev1.StepRequest, flag string) (*modulev1.StepResult, error) {
	flags := sdk.StateMap(req.GetState())
	flags[flag] = true
	s, err := sdk.NewState(flags)
	if err != nil {
		return nil, err
	}
	return &modulev1.StepResult{Status: modulev1.StepResult_STATUS_OK, State: s}, nil
}

func boolFlag(flags map[string]any, key string) bool {
	v, _ := flags[key].(bool)
	return v
}

// --- secrets ------------------------------------------------------------

func (m *stepCAModule) ensurePassword(ctx context.Context) (string, error) {
	if err := m.dial(); err != nil {
		return "", err
	}
	if _, err := m.secretsClient.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref:       caPasswordRef,
		Generator: secretsv1.Generator_GENERATOR_PASSWORD,
		Meta:      &secretsv1.Meta{Owner: "step-ca", Consumers: []string{"step-ca"}, Kind: "ca-password", Recovery: true},
	}); err != nil {
		return "", fmt.Errorf("génération du mot de passe de la CA : %w", err)
	}
	resp, err := m.secretsClient.Get(ctx, &secretsv1.GetRequest{Ref: caPasswordRef})
	if err != nil {
		return "", fmt.Errorf("lecture du mot de passe de la CA : %w", err)
	}
	return resp.GetValue(), nil
}

func (m *stepCAModule) getSecret(ctx context.Context, ref string) (string, error) {
	if err := m.dial(); err != nil {
		return "", err
	}
	resp, err := m.secretsClient.Get(ctx, &secretsv1.GetRequest{Ref: ref})
	if err != nil {
		return "", err
	}
	return resp.GetValue(), nil
}

// putSecret stocke via Put (pas de générateur standard pour une CA générée
// par un produit tiers, docs06 : "le certificat n'est pas dans cette
// liste"), recovery:true — la racine step-ca ne migre jamais vers vault
// (docs06 : "les entrées Recovery: true restent dans file").
func (m *stepCAModule) putSecret(ctx context.Context, ref, value, kind string) error {
	if err := m.dial(); err != nil {
		return err
	}
	_, err := m.secretsClient.Put(ctx, &secretsv1.PutRequest{
		Ref: ref, Value: value,
		Meta: &secretsv1.Meta{Owner: "step-ca", Consumers: []string{"step-ca"}, Kind: kind, Recovery: true},
	})
	return err
}

// --- conteneur step-ca (CLI `step`, mode local/hors-ligne) ---------------

// runStep exécute une commande dans un conteneur step-ca jetable. files
// (chemin relatif à /pki -> contenu) y sont déposés avant l'exécution et
// collect (chemins relatifs à /pki) relus après : core.container/v1 les fait
// transiter par la couche du conteneur, jamais par un répertoire de la graine
// (clé de la CA racine, mot de passe).
func (m *stepCAModule) runStep(ctx context.Context, files map[string]string, collect []string, args []string) (stdout string, collected map[string]string, err error) {
	if err := m.dial(); err != nil {
		return "", nil, err
	}
	req := &containerv1.RunRequest{Image: stepCAImage, Command: args}
	for name, content := range files {
		req.Files = append(req.Files, &containerv1.File{Path: "/pki/" + name, Content: []byte(content)})
	}
	for _, name := range collect {
		req.Collect = append(req.Collect, "/pki/"+name)
	}
	resp, err := m.containerClient.Run(ctx, req)
	if err != nil {
		return "", nil, fmt.Errorf("step %v : %w", args, err)
	}
	if resp.GetExitCode() != 0 {
		return "", nil, fmt.Errorf("step %v (code %d) :\n%s\n%s", args, resp.GetExitCode(), resp.GetStdout(), resp.GetStderr())
	}
	collected = map[string]string{}
	for _, f := range resp.GetCollected() {
		collected[strings.TrimPrefix(f.GetPath(), "/pki/")] = string(f.GetContent())
	}
	for _, name := range collect {
		if _, ok := collected[name]; !ok {
			return "", nil, fmt.Errorf("step %v : fichier %s absent après exécution", args, name)
		}
	}
	return resp.GetStdout(), collected, nil
}

// --- CA racine et intermédiaire ------------------------------------------

func (m *stepCAModule) ensureRoot(ctx context.Context, password string) (certPEM, keyPEM string, err error) {
	cert, certErr := m.getSecret(ctx, rootCertRef)
	key, keyErr := m.getSecret(ctx, rootKeyRef)
	if certErr == nil && keyErr == nil {
		return cert, key, nil
	}

	// --profile root-ca (par défaut) pose pathlen:1 sur la racine, ce qui
	// suffit pour signer UN intermédiaire mais pas pour qu'un pki.issuer/v1
	// tiers (ex. vault) obtienne à son tour un intermédiaire signé par
	// celui de step-ca — vérifié manuellement dans Docker avant d'écrire ce
	// code. --template est le seul moyen de contrôler maxPathLen sur une
	// racine auto-signée (incompatible avec --profile).
	_, out, err := m.runStep(ctx,
		map[string]string{"password": password, "root.tpl": rootTemplate},
		[]string{"root_ca.crt", "root_ca_key"},
		[]string{
			"step", "certificate", "create", "Genesis Root CA", "/pki/root_ca.crt", "/pki/root_ca_key",
			"--template", "/pki/root.tpl", "--password-file", "/pki/password", "--force",
		})
	if err != nil {
		return "", "", fmt.Errorf("génération de la CA racine : %w", err)
	}
	cert, key = out["root_ca.crt"], out["root_ca_key"]
	if err := m.putSecret(ctx, rootCertRef, cert, "ca-root-cert"); err != nil {
		return "", "", err
	}
	if err := m.putSecret(ctx, rootKeyRef, key, "ca-root-key"); err != nil {
		return "", "", err
	}
	return cert, key, nil
}

func (m *stepCAModule) ensureIntermediate(ctx context.Context, rootCert, rootKey, password string) (certPEM, keyPEM string, err error) {
	cert, certErr := m.getSecret(ctx, intermediateCertRef)
	key, keyErr := m.getSecret(ctx, intermediateKeyRef)
	if certErr == nil && keyErr == nil {
		if expired, err := certExpiresWithin(cert, expiryMargin); err == nil && !expired {
			return cert, key, nil
		}
	}

	m.mu.Lock()
	days := m.intermediateTTLDays
	if days <= 0 {
		days = defaultIntermediateTTLDays
	}
	m.mu.Unlock()

	ttl := fmt.Sprintf("%dh", days*24)
	// create --profile intermediate-ca ne permet pas de fixer pathlen (pas
	// de flag --path-len sur `create`, seulement sur `sign`) : CSR généré
	// séparément puis signé, pour que l'intermédiaire graine puisse à son
	// tour signer l'intermédiaire d'un pki.issuer/v1 tiers (ex. vault) —
	// vérifié manuellement dans Docker avant d'écrire ce code.
	_, csrOut, err := m.runStep(ctx,
		map[string]string{"password": password},
		[]string{"intermediate_ca.csr", "intermediate_ca_key"},
		[]string{
			"step", "certificate", "create", "Genesis Root CA Intermediate", "/pki/intermediate_ca.csr", "/pki/intermediate_ca_key",
			"--csr", "--password-file", "/pki/password", "--force",
		})
	if err != nil {
		return "", "", fmt.Errorf("génération du CSR de l'intermédiaire : %w", err)
	}
	// step certificate sign n'a pas de fichier de sortie positionnel : le
	// certificat signé sort sur stdout (vérifié manuellement, même
	// comportement que pour SignCSR plus bas).
	cert, _, err = m.runStep(ctx,
		map[string]string{
			"password":            password,
			"root_ca.crt":         rootCert,
			"root_ca_key":         rootKey,
			"intermediate_ca.csr": csrOut["intermediate_ca.csr"],
		},
		nil,
		[]string{
			"step", "certificate", "sign", "--profile", "intermediate-ca",
			fmt.Sprintf("--path-len=%d", intermediatePathLen), "--not-before", notBeforeSkew, "--not-after", ttl,
			"/pki/intermediate_ca.csr", "/pki/root_ca.crt", "/pki/root_ca_key", "--password-file", "/pki/password",
		})
	if err != nil {
		return "", "", fmt.Errorf("génération de l'intermédiaire : %w", err)
	}
	key = csrOut["intermediate_ca_key"]
	if err := m.putSecret(ctx, intermediateCertRef, cert, "ca-intermediate-cert"); err != nil {
		return "", "", err
	}
	if err := m.putSecret(ctx, intermediateKeyRef, key, "ca-intermediate-key"); err != nil {
		return "", "", err
	}
	return cert, key, nil
}

func (m *stepCAModule) ensurePKI(ctx context.Context) (pkiMaterial, error) {
	m.mu.Lock()
	if m.pki != nil {
		cached := *m.pki
		m.mu.Unlock()
		if expired, err := certExpiresWithin(cached.IntermediateCertPEM, expiryMargin); err == nil && !expired {
			return cached, nil
		}
	} else {
		m.mu.Unlock()
	}

	password, err := m.ensurePassword(ctx)
	if err != nil {
		return pkiMaterial{}, err
	}
	rootCert, rootKey, err := m.ensureRoot(ctx, password)
	if err != nil {
		return pkiMaterial{}, err
	}
	intCert, intKey, err := m.ensureIntermediate(ctx, rootCert, rootKey, password)
	if err != nil {
		return pkiMaterial{}, err
	}

	material := pkiMaterial{RootCertPEM: rootCert, IntermediateCertPEM: intCert, IntermediateKeyPEM: intKey, Password: password}
	m.mu.Lock()
	m.pki = &material
	m.mu.Unlock()
	return material, nil
}

// intermediateFiles : fichiers nécessaires pour signer avec l'intermédiaire.
func intermediateFiles(material pkiMaterial) map[string]string {
	return map[string]string{
		"password":            material.Password,
		"intermediate_ca.crt": material.IntermediateCertPEM,
		"intermediate_ca_key": material.IntermediateKeyPEM,
	}
}

// --- pki.issuer/v1 --------------------------------------------------------

func durationFlag(ttlSeconds int64) []string {
	if ttlSeconds <= 0 {
		return nil
	}
	return []string{"--not-after", time.Duration(ttlSeconds * int64(time.Second)).String()}
}

func (m *stepCAModule) IssueCert(ctx context.Context, req *pkiissuerv1.IssueCertRequest) (*pkiissuerv1.Certificate, error) {
	material, err := m.ensurePKI(ctx)
	if err != nil {
		return nil, err
	}
	args := []string{
		"step", "certificate", "create", req.GetCommonName(), "/pki/leaf.crt", "/pki/leaf.key",
		"--profile", "leaf", "--not-before", notBeforeSkew,
		"--ca", "/pki/intermediate_ca.crt", "--ca-key", "/pki/intermediate_ca_key", "--ca-password-file", "/pki/password",
		"--no-password", "--insecure", "--force",
	}
	args = append(args, durationFlag(req.GetTtlSeconds())...)
	for _, san := range req.GetSans() {
		args = append(args, "--san", san)
	}
	_, out, err := m.runStep(ctx, intermediateFiles(material), []string{"leaf.crt", "leaf.key"}, args)
	if err != nil {
		return nil, fmt.Errorf("émission du certificat pour %q : %w", req.GetCommonName(), err)
	}
	leafCert, leafKey := out["leaf.crt"], out["leaf.key"]
	return &pkiissuerv1.Certificate{
		CertPem:       leafCert,
		ChainPem:      leafCert + material.IntermediateCertPEM,
		PrivateKeyPem: leafKey,
	}, nil
}

func (m *stepCAModule) SignCSR(ctx context.Context, req *pkiissuerv1.SignCSRRequest) (*pkiissuerv1.Certificate, error) {
	material, err := m.ensurePKI(ctx)
	if err != nil {
		return nil, err
	}
	files := intermediateFiles(material)
	files["csr.pem"] = req.GetCsrPem()

	args := []string{
		"step", "certificate", "sign", "/pki/csr.pem", "/pki/intermediate_ca.crt", "/pki/intermediate_ca_key",
		"--password-file", "/pki/password", "--bundle", "--not-before", notBeforeSkew,
	}
	if req.GetIsCa() {
		// Nécessaire pour qu'un pki.issuer/v1 tiers (ex. vault) obtienne
		// son propre intermédiaire signé par celui-ci, plutôt qu'un
		// certificat feuille (docs07 : "pki_int signé par la racine").
		args = append(args, "--profile", "intermediate-ca", fmt.Sprintf("--path-len=%d", req.GetPathLenConstraint()))
	}
	args = append(args, durationFlag(req.GetTtlSeconds())...)
	// step certificate sign n'a pas de fichier de sortie positionnel : le
	// certificat signé (bundle feuille+intermédiaire) sort sur stdout,
	// vérifié manuellement avant d'écrire ce code.
	stdout, _, err := m.runStep(ctx, files, nil, args)
	if err != nil {
		return nil, fmt.Errorf("signature du CSR : %w", err)
	}
	leafPEM, err := firstPEMBlock(stdout)
	if err != nil {
		return nil, fmt.Errorf("signature du CSR : %w", err)
	}
	return &pkiissuerv1.Certificate{CertPem: leafPEM, ChainPem: stdout}, nil
}

func (m *stepCAModule) SignSSH(context.Context, *pkiissuerv1.SignSSHRequest) (*pkiissuerv1.SSHCertificate, error) {
	return nil, status.Error(codes.Unimplemented, "SignSSH : différé à openssh-bastion (J8), pas encore de consommateur réel à ce jalon (docs/PROGRESS.md)")
}

func (m *stepCAModule) CAChain(ctx context.Context, _ *pkiissuerv1.Empty) (*pkiissuerv1.CAChainResponse, error) {
	material, err := m.ensurePKI(ctx)
	if err != nil {
		return nil, err
	}
	return &pkiissuerv1.CAChainResponse{ChainPem: material.IntermediateCertPEM + material.RootCertPEM}, nil
}

// --- utilitaires X.509 ----------------------------------------------------

func firstPEMBlock(data string) (string, error) {
	block, _ := pem.Decode([]byte(data))
	if block == nil {
		return "", fmt.Errorf("aucun bloc PEM trouvé")
	}
	return string(pem.EncodeToMemory(block)), nil
}

func certExpiresWithin(certPEM string, margin time.Duration) (bool, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return false, fmt.Errorf("certificat illisible")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}
	return time.Now().Add(margin).After(cert.NotAfter), nil
}

// verifyChain vérifie cryptographiquement que chainPEM (feuille +
// intermédiaire(s)) remonte à rootPEM.
func verifyChain(chainPEM, rootPEM string) error {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(rootPEM)) {
		return fmt.Errorf("racine illisible")
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
			return err
		}
		if leaf == nil {
			leaf = cert
			continue
		}
		intermediates.AddCert(cert)
	}
	if leaf == nil {
		return fmt.Errorf("aucun certificat feuille dans la chaîne")
	}
	_, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	return err
}

// --- adaptateur grpc -------------------------------------------------------

type pkiIssuerServer struct {
	pkiissuerv1.UnimplementedPkiIssuerServer
	module *stepCAModule
}

func (s *pkiIssuerServer) IssueCert(ctx context.Context, req *pkiissuerv1.IssueCertRequest) (*pkiissuerv1.Certificate, error) {
	return s.module.IssueCert(ctx, req)
}

func (s *pkiIssuerServer) SignCSR(ctx context.Context, req *pkiissuerv1.SignCSRRequest) (*pkiissuerv1.Certificate, error) {
	return s.module.SignCSR(ctx, req)
}

func (s *pkiIssuerServer) SignSSH(ctx context.Context, req *pkiissuerv1.SignSSHRequest) (*pkiissuerv1.SSHCertificate, error) {
	return s.module.SignSSH(ctx, req)
}

func (s *pkiIssuerServer) CAChain(ctx context.Context, req *pkiissuerv1.Empty) (*pkiissuerv1.CAChainResponse, error) {
	return s.module.CAChain(ctx, req)
}

func main() {
	mf, err := sdk.ParseManifest(manifestYAML)
	if err != nil {
		panic(err)
	}
	module := &stepCAModule{manifest: mf.ToProto()}
	sdk.Serve(module, sdk.FunctionProvider{
		Name:     "pki.issuer/v1",
		Register: func(s *grpc.Server) { pkiissuerv1.RegisterPkiIssuerServer(s, &pkiIssuerServer{module: module}) },
	})
}
