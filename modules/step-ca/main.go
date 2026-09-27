// SPDX-License-Identifier: Apache-2.0

// step-ca provides pki.issuer/v1 in the seed phase (docs/07-mvp-modules.md): a
// root CA (stored as recovery through core.secrets) + a seed intermediate,
// issuing/signing certificates by really driving the `step` CLI of the
// smallstep/step-ca container (docs/03-module-contract.md rule 8:
// "orchestrate, don't reinvent"). Purely local/offline signing (`step
// certificate create`/`sign` with the CA files mounted) — no detached step-ca
// server: this avoids the TLS/DNS/provisioner complexity of the product's
// network mode, which this milestone does not need (nothing consumes step-ca's
// HTTP API yet). SignSSH stays an explicit stub (Unimplemented): nothing
// consumes it before openssh-bastion (M8), where it will be built and really
// tested against a real consumer — the same method as Harden in
// modules/base-os (M5).
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
	// expiryMargin: regenerates the intermediate a little before its real
	// expiry rather than waiting for the exact deadline (debt: no
	// scheduled/periodic renewal, only detected on the next call —
	// docs/PROGRESS.md).
	expiryMargin = 24 * time.Hour
	// intermediatePathLen: how many further levels of intermediates step-ca's
	// seed intermediate may itself sign — 1, so that a third-party
	// pki.issuer/v1 (e.g. vault) can get ITS own intermediate (pathlen 0,
	// leaves only) signed by this one. The root must therefore allow at least
	// 2 levels (root → this intermediate → the third party's intermediate),
	// checked by hand in Docker: --profile root-ca (pathlen:1 by default) is
	// not enough.
	intermediatePathLen = 1
	// notBeforeSkew: moves the start of validity of each issued/signed
	// certificate slightly back, to avoid a "notBefore before signer's
	// notBefore" error on the consumer side (e.g. Vault) when its clock or its
	// issuance follows the signing of its own intermediate by a few seconds —
	// observed by hand, standard PKI practice.
	notBeforeSkew = "-1m"
)

// rootTemplate sets maxPathLen on the self-signed root — --template is the
// only way to do so (incompatible with --profile root-ca, which enforces
// pathlen:1).
const rootTemplate = `{
  "subject": {"commonName": "Genesis Root CA"},
  "issuer": {"commonName": "Genesis Root CA"},
  "keyUsage": ["certSign", "crlSign"],
  "basicConstraints": {"isCA": true, "maxPathLen": 2}
}`

// pkiMaterial is the active PKI material, cached in memory once
// loaded/generated — the same debt as modules/coredns and modules/powerdns
// (docs/03 §2: no local state, but nothing passes this state to the function
// handlers, which have no StepRequest.state).
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
		return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_COMPLIANT}, nil
	}
	return &modulev1.CheckResult{Status: modulev1.CheckResult_STATUS_TODO}, nil
}

// dial dials the broker session at most once (Dial only succeeds once per
// token) — the same precaution as modules/chrony.
func (m *stepCAModule) dial() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.containerClient != nil {
		return nil
	}
	if m.broker == nil || m.brokerToken == "" {
		return fmt.Errorf("step-ca: no broker session (Check has not been called yet)")
	}
	conn, err := m.broker.Dial(m.brokerToken)
	if err != nil {
		return fmt.Errorf("connecting to the required functions: %w", err)
	}
	m.containerClient = containerv1.NewContainerClient(conn)
	m.secretsClient = secretsv1.NewSecretsClient(conn)
	return nil
}

// SeedUp bootstraps the CA (root + intermediate): this is the only lifecycle
// milestone of a pure seed module (docs/03 §5), and Verify must be able to
// issue a certificate right after, so the work happens here, not lazily on the
// first call (unlike modules/coredns).
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

// Verify issues a test certificate and validates its chain cryptographically
// (docs/03 §4 rule 2) — pki.issuer/v1 is the function itself: no third party
// on the network is needed, checking the chain directly IS the most direct
// consumer test possible here.
func (m *stepCAModule) Verify(ctx context.Context, req *modulev1.StepRequest) (*modulev1.StepResult, error) {
	cert, err := m.IssueCert(ctx, &pkiissuerv1.IssueCertRequest{
		CommonName: "verify.step-ca.internal",
		TtlSeconds: int64((24 * time.Hour).Seconds()),
	})
	if err != nil {
		return nil, fmt.Errorf("Verify(step-ca): %w", err)
	}
	material, err := m.ensurePKI(ctx)
	if err != nil {
		return nil, err
	}
	if err := verifyChain(cert.GetChainPem(), material.RootCertPEM); err != nil {
		return nil, fmt.Errorf("Verify(step-ca): invalid chain: %w", err)
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
		return "", fmt.Errorf("generating the CA password: %w", err)
	}
	resp, err := m.secretsClient.Get(ctx, &secretsv1.GetRequest{Ref: caPasswordRef})
	if err != nil {
		return "", fmt.Errorf("reading the CA password: %w", err)
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

// putSecret stores through Put (no standard generator for a CA generated by a
// third-party product, doc 06: "the certificate is not in this list"),
// recovery:true — the step-ca root never migrates to vault (doc 06: "Recovery:
// true entries stay in file").
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

// --- step-ca container (`step` CLI, local/offline mode) -------------------

// runStep runs a command in a disposable step-ca container. files (path
// relative to /pki -> content) are placed there before the run and collect
// (paths relative to /pki) read back afterwards: core.container/v1 passes them
// through the container's layer, never through a directory of the seed (root
// CA key, password).
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
		return "", nil, fmt.Errorf("step %v: %w", args, err)
	}
	if resp.GetExitCode() != 0 {
		return "", nil, fmt.Errorf("step %v (code %d):\n%s\n%s", args, resp.GetExitCode(), resp.GetStdout(), resp.GetStderr())
	}
	collected = map[string]string{}
	for _, f := range resp.GetCollected() {
		collected[strings.TrimPrefix(f.GetPath(), "/pki/")] = string(f.GetContent())
	}
	for _, name := range collect {
		if _, ok := collected[name]; !ok {
			return "", nil, fmt.Errorf("step %v: file %s missing after the run", args, name)
		}
	}
	return resp.GetStdout(), collected, nil
}

// --- Root and intermediate CA ---------------------------------------------

func (m *stepCAModule) ensureRoot(ctx context.Context, password string) (certPEM, keyPEM string, err error) {
	cert, certErr := m.getSecret(ctx, rootCertRef)
	key, keyErr := m.getSecret(ctx, rootKeyRef)
	if certErr == nil && keyErr == nil {
		return cert, key, nil
	}

	// --profile root-ca (the default) sets pathlen:1 on the root, which is
	// enough to sign ONE intermediate but not for a third-party pki.issuer/v1
	// (e.g. vault) to get an intermediate signed in turn by step-ca's —
	// checked by hand in Docker before writing this code. --template is the
	// only way to control maxPathLen on a self-signed root (incompatible with
	// --profile).
	_, out, err := m.runStep(ctx,
		map[string]string{"password": password, "root.tpl": rootTemplate},
		[]string{"root_ca.crt", "root_ca_key"},
		[]string{
			"step", "certificate", "create", "Genesis Root CA", "/pki/root_ca.crt", "/pki/root_ca_key",
			"--template", "/pki/root.tpl", "--password-file", "/pki/password", "--force",
		})
	if err != nil {
		return "", "", fmt.Errorf("generating the root CA: %w", err)
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
	// create --profile intermediate-ca cannot set pathlen (no --path-len flag
	// on `create`, only on `sign`): the CSR is generated separately then
	// signed, so that the seed intermediate can in turn sign the intermediate
	// of a third-party pki.issuer/v1 (e.g. vault) — checked by hand in Docker
	// before writing this code.
	_, csrOut, err := m.runStep(ctx,
		map[string]string{"password": password},
		[]string{"intermediate_ca.csr", "intermediate_ca_key"},
		[]string{
			"step", "certificate", "create", "Genesis Root CA Intermediate", "/pki/intermediate_ca.csr", "/pki/intermediate_ca_key",
			"--csr", "--password-file", "/pki/password", "--force",
		})
	if err != nil {
		return "", "", fmt.Errorf("generating the intermediate's CSR: %w", err)
	}
	// step certificate sign has no positional output file: the signed
	// certificate goes to stdout (checked by hand, the same behaviour as for
	// SignCSR below).
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
		return "", "", fmt.Errorf("generating the intermediate: %w", err)
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

// intermediateFiles: the files needed to sign with the intermediate.
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
		return nil, fmt.Errorf("issuing the certificate for %q: %w", req.GetCommonName(), err)
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
		// Needed for a third-party pki.issuer/v1 (e.g. vault) to get its own
		// intermediate signed by this one, rather than a leaf certificate (doc
		// 07: "pki_int signed by the root").
		args = append(args, "--profile", "intermediate-ca", fmt.Sprintf("--path-len=%d", req.GetPathLenConstraint()))
	}
	args = append(args, durationFlag(req.GetTtlSeconds())...)
	// step certificate sign has no positional output file: the signed
	// certificate (leaf+intermediate bundle) goes to stdout, checked by hand
	// before writing this code.
	stdout, _, err := m.runStep(ctx, files, nil, args)
	if err != nil {
		return nil, fmt.Errorf("signing the CSR: %w", err)
	}
	leafPEM, err := firstPEMBlock(stdout)
	if err != nil {
		return nil, fmt.Errorf("signing the CSR: %w", err)
	}
	return &pkiissuerv1.Certificate{CertPem: leafPEM, ChainPem: stdout}, nil
}

func (m *stepCAModule) SignSSH(context.Context, *pkiissuerv1.SignSSHRequest) (*pkiissuerv1.SSHCertificate, error) {
	return nil, status.Error(codes.Unimplemented, "SignSSH: deferred, no real consumer yet (docs/PROGRESS.md)")
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
		return "", fmt.Errorf("no PEM block found")
	}
	return string(pem.EncodeToMemory(block)), nil
}

func certExpiresWithin(certPEM string, margin time.Duration) (bool, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return false, fmt.Errorf("unreadable certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false, err
	}
	return time.Now().Add(margin).After(cert.NotAfter), nil
}

// verifyChain checks cryptographically that chainPEM (leaf + intermediate(s))
// chains up to rootPEM.
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
		return fmt.Errorf("no leaf certificate in the chain")
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
