// SPDX-License-Identifier: Apache-2.0

//go:build docker

package integration

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/testutil"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	dnsresolverv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	osbasev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	pkiissuerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/pki/issuer/v1"
	timentpv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// Teleport, like vault, cannot be installed with apt+systemd on the disposable
// SSH containers (Alpine, no systemd) used elsewhere in this repository:
// teleportFakeAnsibleServer therefore simulates "what ansible would have done"
// by driving two REAL Teleport containers directly (auth+proxy, then agent)
// with the REAL certificates/tokens/pins the teleport module passed to it --
// everything else (startup, node join, user certificate issuance, SSH
// connection through the agent) runs for real, against a real Teleport. The
// same method as internal/modulehost/vault_test.go.
//
// Image: teleportTestImage (teleport_image_test.go), built locally with the
// same major version as the one the module installs
// (playbooks/install_teleport.yml, install_agent.yml: stable/v17 channel).
const (
	teleportNodePort = "13022"
	// teleportTestSSHUser must match sshUser (modules/teleport/main.go):
	// internal/ cannot import modules/, so this constant is duplicated here to
	// create the OS account expected in the agent container.
	teleportTestSSHUser = "genesis"
)

type teleportFakeComputeVMServer struct {
	computevmv1.UnimplementedComputeVMServer
	mu  sync.Mutex
	vms map[string]*computevmv1.VM
}

func newTeleportFakeComputeVMServer() *teleportFakeComputeVMServer {
	return &teleportFakeComputeVMServer{vms: map[string]*computevmv1.VM{}}
}

func (f *teleportFakeComputeVMServer) EnsureVM(_ context.Context, req *computevmv1.EnsureVMRequest) (*computevmv1.VM, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if vm, ok := f.vms[req.GetName()]; ok {
		return vm, nil
	}
	vm := &computevmv1.VM{Id: "vm-" + req.GetName(), Name: req.GetName(), Ip: "127.0.0.1", Status: "running", SshPort: 22}
	f.vms[req.GetName()] = vm
	return vm, nil
}

func (f *teleportFakeComputeVMServer) DeleteVM(_ context.Context, req *computevmv1.DeleteVMRequest) (*computevmv1.DeleteVMResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.vms, req.GetName())
	return &computevmv1.DeleteVMResponse{}, nil
}

type teleportFakeOSBaseServer struct {
	osbasev1.UnimplementedBaseServer
}

func (teleportFakeOSBaseServer) SetNTP(context.Context, *osbasev1.SetNTPRequest) (*osbasev1.SetNTPResponse, error) {
	return &osbasev1.SetNTPResponse{}, nil
}
func (teleportFakeOSBaseServer) SetResolver(context.Context, *osbasev1.SetResolverRequest) (*osbasev1.SetResolverResponse, error) {
	return &osbasev1.SetResolverResponse{}, nil
}

type teleportFakeTimeNTPServer struct {
	timentpv1.UnimplementedTimeNTPServer
}

func (teleportFakeTimeNTPServer) Endpoint(context.Context, *timentpv1.Empty) (*timentpv1.EndpointInfo, error) {
	return &timentpv1.EndpointInfo{Address: "10.10.0.9", Port: 123}, nil
}

type teleportFakeDnsResolverServer struct {
	dnsresolverv1.UnimplementedDnsResolverServer
}

func (teleportFakeDnsResolverServer) Endpoint(context.Context, *dnsresolverv1.Empty) (*dnsresolverv1.EndpointInfo, error) {
	return &dnsresolverv1.EndpointInfo{Address: "10.10.0.8", Port: 53}, nil
}

type teleportFakeAnsibleServer struct {
	ansiblev1.UnimplementedAnsibleServer
	t     *testing.T
	image string // teleportTestImage
	mu    sync.Mutex
	dir   string
	net   string
	auth  string // name of the auth+proxy container
	agent string // name of the agent container (once installed)
	calls int
}

func (f *teleportFakeAnsibleServer) RunPlaybook(_ context.Context, req *ansiblev1.RunPlaybookRequest) (*ansiblev1.RunPlaybookResponse, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	playbook := string(req.GetPlaybookYaml())
	vars := req.GetVars().AsMap()

	var (
		out string
		err error
	)
	switch {
	case strings.Contains(playbook, "configure teleport.yaml (Auth + Proxy)"):
		out, err = f.deployAuthProxy(vars)
	case strings.Contains(playbook, "generate a node join token"):
		out, err = f.newToken(vars)
	case strings.Contains(playbook, "configure teleport.yaml (SSH agent only)"):
		out, err = f.deployAgent(vars)
	case strings.Contains(playbook, "sign a short-lived SSH certificate"):
		out, err = f.generateUserCert(vars)
	case strings.Contains(playbook, "connect through the teleport agent"):
		out, err = f.checkSSH(vars)
	default:
		return &ansiblev1.RunPlaybookResponse{Ok: false, Output: "playbook unknown to the fake"}, nil
	}
	if err != nil {
		return &ansiblev1.RunPlaybookResponse{Ok: false, Output: out + "\n" + err.Error()}, nil
	}
	return &ansiblev1.RunPlaybookResponse{Ok: true, Output: out}, nil
}

func (f *teleportFakeAnsibleServer) ensureDirAndNetwork() error {
	if f.dir == "" {
		dir, err := os.MkdirTemp("", "genesis-teleport-test-*")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir+"/auth/data", 0o777); err != nil {
			return err
		}
		if err := os.MkdirAll(dir+"/agent/data", 0o777); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o777); err != nil {
			return err
		}
		if err := os.Chmod(dir+"/auth", 0o777); err != nil {
			return err
		}
		if err := os.Chmod(dir+"/auth/data", 0o777); err != nil {
			return err
		}
		if err := os.Chmod(dir+"/agent", 0o777); err != nil {
			return err
		}
		if err := os.Chmod(dir+"/agent/data", 0o777); err != nil {
			return err
		}
		f.dir = dir
	}
	if f.net == "" {
		name := "genesis-teleport-test-net"
		_ = exec.Command("docker", "network", "rm", name).Run()
		if out, err := exec.Command("docker", "network", "create", name).CombinedOutput(); err != nil {
			return fmt.Errorf("docker network create: %w: %s", err, out)
		}
		f.net = name
	}
	return nil
}

// deployAuthProxy drives Docker directly with the REAL TLS certificates
// received from the module (issued by the real step-ca) -- the real equivalent
// of what install_teleport.yml would do on a VM.
func (f *teleportFakeAnsibleServer) deployAuthProxy(vars map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.ensureDirAndNetwork(); err != nil {
		return "", err
	}
	certPEM, _ := vars["tls_cert_pem"].(string)
	keyPEM, _ := vars["tls_key_pem"].(string)
	caChainPEM, _ := vars["ca_chain_pem"].(string)
	clusterName, _ := vars["cluster_name"].(string)
	if certPEM == "" || keyPEM == "" || caChainPEM == "" || clusterName == "" {
		return "", fmt.Errorf("missing tls_cert_pem/tls_key_pem/ca_chain_pem/cluster_name vars")
	}
	if err := os.WriteFile(f.dir+"/auth/proxy-cert.pem", []byte(certPEM), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(f.dir+"/auth/proxy-key.pem", []byte(keyPEM), 0o644); err != nil {
		return "", err
	}
	// The container does not trust the pki.issuer/v1 root (the test step-ca)
	// by default: teleport's Proxy Service validates its own TLS chain at
	// startup against the system store -- the same mechanism as
	// playbooks/install_teleport.yml (update-ca-certificates), through
	// SSL_CERT_FILE here (suggested by teleport's own error message).
	if err := os.WriteFile(f.dir+"/auth/ca-chain.pem", []byte(caChainPEM), 0o644); err != nil {
		return "", err
	}
	cfg := fmt.Sprintf(`version: v3
teleport:
  nodename: teleport-auth-test
  data_dir: /var/lib/teleport
auth_service:
  enabled: true
  cluster_name: %s
  listen_addr: 0.0.0.0:3025
  proxy_listener_mode: multiplex
proxy_service:
  enabled: true
  web_listen_addr: 0.0.0.0:3080
  public_addr: "127.0.0.1:3080"
  https_keypairs:
    - key_file: /etc/teleport/proxy-key.pem
      cert_file: /etc/teleport/proxy-cert.pem
ssh_service:
  enabled: false
`, clusterName)
	if err := os.WriteFile(f.dir+"/auth/teleport.yaml", []byte(cfg), 0o644); err != nil {
		return "", err
	}

	name := "genesis-teleport-test-auth"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	cmd := exec.Command("docker", "run", "-d", "--name", name, "--network", f.net,
		"--entrypoint", "teleport",
		"-e", "SSL_CERT_FILE=/etc/teleport/ca-chain.pem",
		"-v", f.dir+"/auth:/etc/teleport",
		"-v", f.dir+"/auth/data:/var/lib/teleport",
		f.image, "start", "--config=/etc/teleport/teleport.yaml")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker run teleport (auth+proxy): %w", err)
	}
	f.auth = name

	statusOut, err := f.waitTctl(name, "status")
	if err != nil {
		return string(out) + "\n" + statusOut, err
	}
	return string(out) + "\n" + statusOut, nil
}

// waitTctl retries a tctl command until the Auth API answers (the container
// has just started) -- the same precaution as modules/vault.waitForVault,
// checked by hand in Docker.
func (f *teleportFakeAnsibleServer) waitTctl(container string, args ...string) (string, error) {
	fullArgs := append([]string{"exec", container, "tctl"}, append(args, "--config=/etc/teleport/teleport.yaml")...)
	deadline := time.Now().Add(60 * time.Second)
	var lastOut []byte
	var lastErr error
	for time.Now().Before(deadline) {
		out, err := exec.Command("docker", fullArgs...).CombinedOutput()
		if err == nil {
			return string(out), nil
		}
		lastOut, lastErr = out, err
		time.Sleep(2 * time.Second)
	}
	return string(lastOut), fmt.Errorf("tctl %v is not answering after 60s: %w", args, lastErr)
}

// newToken retries like waitTctl (even though "tctl status" already succeeded
// earlier in Configure, "tctl tokens add" can fail transiently under load --
// observed while running the whole module suite in parallel on this machine).
func (f *teleportFakeAnsibleServer) newToken(_ map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out, err := f.waitTctl(f.auth, "tokens", "add", "--type=node", "--ttl=10m")
	if err != nil {
		return out, fmt.Errorf("tctl tokens add: %w", err)
	}
	return out, nil
}

// deployAgent drives Docker directly with the REAL token/pin/address received
// from the module -- the real equivalent of install_agent.yml, it joins the
// cluster through the dedicated docker network (DNS resolution by container
// name).
func (f *teleportFakeAnsibleServer) deployAgent(vars map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	joinToken, _ := vars["join_token"].(string)
	caPin, _ := vars["ca_pin"].(string)
	if joinToken == "" || caPin == "" {
		return "", fmt.Errorf("missing join_token/ca_pin vars")
	}
	authServer := f.auth + ":3025" // ownTarget.Host is 127.0.0.1 on the fake VM side: ignore the received value and resolve through the dedicated docker network.

	cfg := fmt.Sprintf(`version: v3
teleport:
  nodename: teleport-agent-test
  data_dir: /var/lib/teleport
  join_params:
    token_name: %s
    method: token
  ca_pin: %s
  auth_server: %s
auth_service:
  enabled: false
proxy_service:
  enabled: false
ssh_service:
  enabled: true
  listen_addr: 0.0.0.0:3022
`, joinToken, caPin, authServer)
	if err := os.WriteFile(f.dir+"/agent/teleport.yaml", []byte(cfg), 0o644); err != nil {
		return "", err
	}

	name := "genesis-teleport-test-agent"
	_ = exec.Command("docker", "rm", "-f", name).Run()
	cmd := exec.Command("docker", "run", "-d", "--name", name, "--network", f.net,
		"--entrypoint", "teleport",
		"-p", "127.0.0.1:"+teleportNodePort+":3022",
		"-v", f.dir+"/agent:/etc/teleport",
		"-v", f.dir+"/agent/data:/var/lib/teleport",
		f.image, "start", "--config=/etc/teleport/teleport.yaml")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker run teleport (agent): %w", err)
	}
	f.agent = name

	// The agent container is a Debian image with teleport, without the
	// "genesis" user a real VM (os.base/v1) would already have created --
	// teleport's ssh_service needs a real local OS account to open a session.
	if userOut, err := exec.Command("docker", "exec", name, "useradd", "-m", "-s", "/bin/bash", teleportTestSSHUser).CombinedOutput(); err != nil {
		return string(out) + "\n" + string(userOut), fmt.Errorf("creating user %q in the agent container: %w", teleportTestSSHUser, err)
	}

	deadline := time.Now().Add(60 * time.Second)
	var lastOut []byte
	for time.Now().Before(deadline) {
		// --format=json: the default table of "tctl nodes ls" truncates the
		// host name ("teleport-agent-t..."), which breaks the substring search
		// below.
		nodesOut, nodesErr := exec.Command("docker", "exec", f.auth, "tctl", "nodes", "ls", "--format=json",
			"--config=/etc/teleport/teleport.yaml").CombinedOutput()
		if nodesErr == nil && strings.Contains(string(nodesOut), "teleport-agent-test") {
			return string(out) + "\n" + string(nodesOut), nil
		}
		lastOut = nodesOut
		time.Sleep(2 * time.Second)
	}
	return string(out) + "\n" + string(lastOut), fmt.Errorf("the agent never joined the cluster after 60s")
}

// generateUserCert creates the verification user and signs its SSH certificate
// with the REAL tctl auth sign, on the auth container -- the real equivalent
// of playbooks/generate_user_cert.yml.
func (f *teleportFakeAnsibleServer) generateUserCert(vars map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	verifyUser, _ := vars["verify_user"].(string)
	sshLogin, _ := vars["ssh_login"].(string)
	if verifyUser == "" || sshLogin == "" {
		return "", fmt.Errorf("missing verify_user/ssh_login vars")
	}

	listOut, _ := exec.Command("docker", "exec", f.auth, "tctl", "users", "ls",
		"--config=/etc/teleport/teleport.yaml").CombinedOutput()
	if !strings.Contains(string(listOut), verifyUser) {
		addOut, err := exec.Command("docker", "exec", f.auth, "tctl", "users", "add", verifyUser,
			"--roles=access", "--logins="+sshLogin, "--config=/etc/teleport/teleport.yaml").CombinedOutput()
		if err != nil {
			return string(addOut), fmt.Errorf("tctl users add: %w", err)
		}
	}

	signOut, err := exec.Command("docker", "exec", f.auth, "tctl", "auth", "sign",
		"--user="+verifyUser, "--out=/tmp/genesis-verify", "--ttl=5m", "--format=openssh", "--overwrite",
		"--config=/etc/teleport/teleport.yaml").CombinedOutput()
	if err != nil {
		return string(signOut), fmt.Errorf("tctl auth sign: %w", err)
	}

	priv, err := exec.Command("docker", "exec", f.auth, "cat", "/tmp/genesis-verify").Output()
	if err != nil {
		return string(signOut), fmt.Errorf("reading the generated private key: %w", err)
	}
	cert, err := exec.Command("docker", "exec", f.auth, "cat", "/tmp/genesis-verify-cert.pub").Output()
	if err != nil {
		return string(signOut), fmt.Errorf("reading the generated certificate: %w", err)
	}

	return fmt.Sprintf("PRIVATE_KEY_B64:%s\nCERTIFICATE_B64:%s",
		base64.StdEncoding.EncodeToString(priv), base64.StdEncoding.EncodeToString(cert)), nil
}

// checkSSH really opens, from the test process (the "verifier VM" point of
// view), an SSH connection through the Teleport agent (node port published on
// the host) with the received certificate -- the real equivalent of
// playbooks/check_ssh.yml.
func (f *teleportFakeAnsibleServer) checkSSH(vars map[string]any) (string, error) {
	privPEM, _ := vars["user_private_key_pem"].(string)
	certOpenSSH, _ := vars["user_certificate_openssh"].(string)
	agentUser, _ := vars["agent_user"].(string)
	if privPEM == "" || certOpenSSH == "" || agentUser == "" {
		return "", fmt.Errorf("missing user_private_key_pem/user_certificate_openssh/agent_user vars")
	}

	f.mu.Lock()
	dir := f.dir
	f.mu.Unlock()

	keyPath := dir + "/verify-key"
	if err := os.WriteFile(keyPath, []byte(privPEM), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(keyPath+"-cert.pub", []byte(certOpenSSH), 0o644); err != nil {
		return "", err
	}

	deadline := time.Now().Add(30 * time.Second)
	var out []byte
	var err error
	for time.Now().Before(deadline) {
		cmd := exec.Command("ssh",
			"-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null",
			"-o", "BatchMode=yes", "-o", "ConnectTimeout=5",
			"-i", keyPath, "-p", teleportNodePort,
			agentUser+"@127.0.0.1", "echo", "AGENT_SSH_OK")
		out, err = cmd.CombinedOutput()
		if err == nil {
			return string(out), nil
		}
		time.Sleep(2 * time.Second)
	}
	return string(out), fmt.Errorf("SSH connection through the agent failed: %w", err)
}

func (f *teleportFakeAnsibleServer) cleanup() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agent != "" {
		_ = exec.Command("docker", "rm", "-f", f.agent).Run()
	}
	if f.auth != "" {
		_ = exec.Command("docker", "rm", "-f", f.auth).Run()
	}
	if f.net != "" {
		_ = exec.Command("docker", "network", "rm", f.net).Run()
	}
	if f.dir != "" {
		_ = os.RemoveAll(f.dir)
	}
}

type teleportTestHandle struct {
	client        *modulehost.Client
	token         string
	configured    *modulev1.StepResult
	ansibleServer *teleportFakeAnsibleServer
}

func launchTeleportTest(t *testing.T) teleportTestHandle {
	t.Helper()
	ctx := context.Background()

	rt := testutil.RequireRuntime(t)
	store := newTestSecretsStore(t)
	registry := broker.NewRegistry()
	registry.SetNative("core.container/v1", broker.NativeContainer(rt))
	registry.SetNative("core.secrets/v1", broker.NativeSecrets(store))

	// Real step-ca: the only credible pki.issuer/v1 provider for this test
	// (the same choice as internal/modulehost/vault_test.go).
	stepCABinary, stepCAManifest := buildModule(t, "step-ca")
	stepCAClient, err := modulehost.Launch(stepCABinary, stepCAManifest)
	if err != nil {
		t.Fatalf("modulehost.Launch(step-ca): %v", err)
	}
	t.Cleanup(stepCAClient.Close)
	stepCAToken := registry.OpenSession(stepCAClient.Broker(), "step-ca", []string{"core.container/v1", "core.secrets/v1"})
	if _, err := stepCAClient.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: stepCAToken}); err != nil {
		t.Fatalf("Check(step-ca): %v", err)
	}
	seedToken := registry.OpenSession(stepCAClient.Broker(), "step-ca", []string{"core.container/v1", "core.secrets/v1"})
	if _, err := stepCAClient.Module().SeedUp(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: seedToken}); err != nil {
		t.Fatalf("SeedUp(step-ca): %v", err)
	}
	stepCAConn, err := stepCAClient.DispenseFunction("pki.issuer/v1")
	if err != nil {
		t.Fatalf("DispenseFunction(step-ca, pki.issuer/v1): %v", err)
	}

	binaryPath, manifest := buildModule(t, "teleport")
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("modulehost.Launch(teleport): %v", err)
	}
	t.Cleanup(client.Close)

	vmServer := newTeleportFakeComputeVMServer()
	ansibleServer := &teleportFakeAnsibleServer{t: t, image: teleportTestImage(t)}
	t.Cleanup(ansibleServer.cleanup)

	sessionID := stepCAClient.Broker().NextId()
	_ = sessionID // NextId() is only a shared counter; we want one on the teleport side.
	teleportSessionID := client.Broker().NextId()
	go client.Broker().AcceptAndServe(teleportSessionID, func(opts []grpc.ServerOption) *grpc.Server {
		s := grpc.NewServer(opts...)
		computevmv1.RegisterComputeVMServer(s, vmServer)
		osbasev1.RegisterBaseServer(s, teleportFakeOSBaseServer{})
		ansiblev1.RegisterAnsibleServer(s, ansibleServer)
		timentpv1.RegisterTimeNTPServer(s, teleportFakeTimeNTPServer{})
		dnsresolverv1.RegisterDnsResolverServer(s, teleportFakeDnsResolverServer{})
		broker.NativeSecrets(store)("teleport")(s)
		pkiissuerv1.RegisterPkiIssuerServer(s, &pkiIssuerForwarder{client: pkiissuerv1.NewPkiIssuerClient(stepCAConn)})
		return s
	})
	token := strconv.FormatUint(uint64(teleportSessionID), 10)

	if _, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token}); err != nil {
		t.Fatalf("Check(teleport): %v", err)
	}
	provisionResp, err := client.Module().Provision(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Provision(teleport): %v", err)
	}
	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: provisionResp.GetState()})
	if err != nil {
		t.Fatalf("Configure(teleport): %v", err)
	}
	if configureResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Configure(teleport).Status = %v", configureResp.GetStatus())
	}

	return teleportTestHandle{client: client, token: token, configured: configureResp, ansibleServer: ansibleServer}
}

// TestTeleportConfiguresAuthProxy proves, against a real Teleport container
// and a real step-ca, the Auth+Proxy deployment (doc 07: TLS issued by
// pki.issuer/v1, CA pin read through tctl status).
func TestTeleportConfiguresAuthProxy(t *testing.T) {
	h := launchTeleportTest(t)
	if h.ansibleServer.calls < 1 {
		t.Error("install_teleport.yml was never sent")
	}
}

// TestTeleportFleetAgentInstallJoinsClusterAndAllowsSSH proves, end to end and
// against real Teleport containers, fleet.agent/v1.Install (ADR-017: enrolment
// token, cluster join) followed by a real SSH connection through the agent
// with a certificate signed by the internal CA (ADR-018) -- exactly the
// scenario tested by hand in Docker before writing this module.
func TestTeleportFleetAgentInstallJoinsClusterAndAllowsSSH(t *testing.T) {
	h := launchTeleportTest(t)
	ctx := context.Background()

	verifyResp, err := h.client.Module().Verify(ctx, &modulev1.StepRequest{
		RunId: "test", BrokerToken: h.token, State: h.configured.GetState(),
	})
	if err != nil {
		t.Fatalf("Verify(teleport): %v", err)
	}
	if verifyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Verify(teleport).Status = %v", verifyResp.GetStatus())
	}
	if h.ansibleServer.calls < 5 {
		t.Errorf("not all of Verify's playbooks were sent (ansible calls = %d)", h.ansibleServer.calls)
	}
}
