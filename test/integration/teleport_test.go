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

// Teleport, comme vault, ne peut pas être installé via apt+systemd sur les
// conteneurs SSH jetables (Alpine, sans systemd) utilisés ailleurs dans ce
// dépôt : teleportFakeAnsibleServer simule donc « ce qu'ansible aurait
// fait » en pilotant directement deux VRAIS conteneurs teleportImage
// (auth+proxy, puis agent) avec les VRAIS certificats/jetons/pins que le
// module teleport lui a transmis -- tout le reste (démarrage, jonction du
// nœud, émission de certificat utilisateur, connexion SSH via l'agent)
// tourne pour de vrai, contre un vrai Teleport. Même méthode que
// internal/modulehost/vault_test.go.
//
// Image : à partir de la v16, gravitational ne publie plus que des images
// "distroless" (aucun shell, /bin/sh absent), ce qui casse l'exécution de
// commande via le ssh_service de l'agent (le shell de connexion de
// l'utilisateur cible n'existe pas dans le conteneur). v14 est la dernière
// version pour laquelle une image classique (avec shell) est publiée sur
// public.ecr.aws/gravitational/teleport ; elle sert ici uniquement de
// doublure de test pour un vrai Teleport -- l'installation réelle du module
// (playbooks/install_teleport.yml, install_agent.yml) cible le dépôt APT
// officiel en canal stable/v17, indépendamment de cette image de test.
const (
	teleportImage    = "public.ecr.aws/gravitational/teleport:14.4.1@sha256:1a0b1561362e5203197908d9a0769078f6df6dd3ae3654697f7000c1280bc293"
	teleportNodePort = "13022"
	// teleportTestSSHUser doit correspondre à sshUser (modules/teleport/main.go) :
	// internal/ ne peut pas importer modules/, cette constante est donc
	// dupliquée ici pour créer le compte OS attendu dans le conteneur agent.
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
	mu    sync.Mutex
	dir   string
	net   string
	auth  string // nom du conteneur auth+proxy
	agent string // nom du conteneur agent (une fois installé)
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
	case strings.Contains(playbook, "configurer teleport.yaml (Auth + Proxy)"):
		out, err = f.deployAuthProxy(vars)
	case strings.Contains(playbook, "generer un jeton d'enrolement de noeud"):
		out, err = f.newToken(vars)
	case strings.Contains(playbook, "configurer teleport.yaml (agent SSH seul)"):
		out, err = f.deployAgent(vars)
	case strings.Contains(playbook, "signer un certificat SSH court terme"):
		out, err = f.generateUserCert(vars)
	case strings.Contains(playbook, "connexion via l'agent teleport"):
		out, err = f.checkSSH(vars)
	default:
		return &ansiblev1.RunPlaybookResponse{Ok: false, Output: "playbook inconnu du fake"}, nil
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
			return fmt.Errorf("docker network create : %w : %s", err, out)
		}
		f.net = name
	}
	return nil
}

// deployAuthProxy pilote directement Docker avec les VRAIS certificats TLS
// reçus du module (émis par le vrai step-ca) -- équivalent réel de ce que
// install_teleport.yml ferait sur une VM.
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
		return "", fmt.Errorf("vars tls_cert_pem/tls_key_pem/ca_chain_pem/cluster_name manquantes")
	}
	if err := os.WriteFile(f.dir+"/auth/proxy-cert.pem", []byte(certPEM), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(f.dir+"/auth/proxy-key.pem", []byte(keyPEM), 0o644); err != nil {
		return "", err
	}
	// Le conteneur ne fait pas confiance à la racine pki.issuer/v1 (step-ca de
	// test) par défaut : la Proxy Service de teleport valide sa propre chaîne
	// TLS au démarrage contre le magasin système -- même mécanisme que
	// playbooks/install_teleport.yml (update-ca-certificates), via
	// SSL_CERT_FILE ici (suggéré par le message d'erreur de teleport lui-même).
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
		teleportImage, "start", "--config=/etc/teleport/teleport.yaml")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker run teleport (auth+proxy) : %w", err)
	}
	f.auth = name

	statusOut, err := f.waitTctl(name, "status")
	if err != nil {
		return string(out) + "\n" + statusOut, err
	}
	return string(out) + "\n" + statusOut, nil
}

// waitTctl réessaie une commande tctl jusqu'à ce que l'API Auth réponde
// (le conteneur vient de démarrer) -- même précaution que
// modules/vault.waitForVault, vérifiée manuellement dans Docker.
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
	return string(lastOut), fmt.Errorf("tctl %v ne répond pas après 60s : %w", args, lastErr)
}

// newToken réessaie comme waitTctl (même si "tctl status" a déjà réussi
// plus tôt dans Configure, "tctl tokens add" peut transitoirement échouer
// sous charge -- observé en exécutant la suite complète des modules en
// parallèle sur cette machine).
func (f *teleportFakeAnsibleServer) newToken(_ map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out, err := f.waitTctl(f.auth, "tokens", "add", "--type=node", "--ttl=10m")
	if err != nil {
		return out, fmt.Errorf("tctl tokens add : %w", err)
	}
	return out, nil
}

// deployAgent pilote directement Docker avec le VRAI jeton/pin/adresse
// reçus du module -- équivalent réel de install_agent.yml, rejoint le
// cluster via le réseau docker dédié (résolution DNS par nom de conteneur).
func (f *teleportFakeAnsibleServer) deployAgent(vars map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	joinToken, _ := vars["join_token"].(string)
	caPin, _ := vars["ca_pin"].(string)
	if joinToken == "" || caPin == "" {
		return "", fmt.Errorf("vars join_token/ca_pin manquantes")
	}
	authServer := f.auth + ":3025" // ownTarget.Host est 127.0.0.1 côté fake VM : on ignore la valeur reçue et on résout via le réseau docker dédié.

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
		teleportImage, "start", "--config=/etc/teleport/teleport.yaml")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker run teleport (agent) : %w", err)
	}
	f.agent = name

	// Le conteneur agent est une image teleport nue, sans l'utilisateur
	// "genesis" qu'une vraie VM (os.base/v1) aurait déjà créé -- ssh_service
	// de teleport a besoin d'un compte OS local réel pour ouvrir une session.
	if userOut, err := exec.Command("docker", "exec", name, "useradd", "-m", "-s", "/bin/bash", teleportTestSSHUser).CombinedOutput(); err != nil {
		return string(out) + "\n" + string(userOut), fmt.Errorf("création de l'utilisateur %q dans le conteneur agent : %w", teleportTestSSHUser, err)
	}

	deadline := time.Now().Add(60 * time.Second)
	var lastOut []byte
	for time.Now().Before(deadline) {
		// --format=json : la table par défaut de "tctl nodes ls" tronque le
		// nom d'hôte ("teleport-agent-t..."), ce qui casse la recherche de
		// substring ci-dessous.
		nodesOut, nodesErr := exec.Command("docker", "exec", f.auth, "tctl", "nodes", "ls", "--format=json",
			"--config=/etc/teleport/teleport.yaml").CombinedOutput()
		if nodesErr == nil && strings.Contains(string(nodesOut), "teleport-agent-test") {
			return string(out) + "\n" + string(nodesOut), nil
		}
		lastOut = nodesOut
		time.Sleep(2 * time.Second)
	}
	return string(out) + "\n" + string(lastOut), fmt.Errorf("l'agent n'a jamais rejoint le cluster après 60s")
}

// generateUserCert crée l'utilisateur de vérification et signe son
// certificat SSH via le VRAI tctl auth sign, sur le conteneur auth --
// équivalent réel de playbooks/generate_user_cert.yml.
func (f *teleportFakeAnsibleServer) generateUserCert(vars map[string]any) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	verifyUser, _ := vars["verify_user"].(string)
	sshLogin, _ := vars["ssh_login"].(string)
	if verifyUser == "" || sshLogin == "" {
		return "", fmt.Errorf("vars verify_user/ssh_login manquantes")
	}

	listOut, _ := exec.Command("docker", "exec", f.auth, "tctl", "users", "ls",
		"--config=/etc/teleport/teleport.yaml").CombinedOutput()
	if !strings.Contains(string(listOut), verifyUser) {
		addOut, err := exec.Command("docker", "exec", f.auth, "tctl", "users", "add", verifyUser,
			"--roles=access", "--logins="+sshLogin, "--config=/etc/teleport/teleport.yaml").CombinedOutput()
		if err != nil {
			return string(addOut), fmt.Errorf("tctl users add : %w", err)
		}
	}

	signOut, err := exec.Command("docker", "exec", f.auth, "tctl", "auth", "sign",
		"--user="+verifyUser, "--out=/tmp/genesis-verify", "--ttl=5m", "--format=openssh", "--overwrite",
		"--config=/etc/teleport/teleport.yaml").CombinedOutput()
	if err != nil {
		return string(signOut), fmt.Errorf("tctl auth sign : %w", err)
	}

	priv, err := exec.Command("docker", "exec", f.auth, "cat", "/tmp/genesis-verify").Output()
	if err != nil {
		return string(signOut), fmt.Errorf("lecture de la clé privée générée : %w", err)
	}
	cert, err := exec.Command("docker", "exec", f.auth, "cat", "/tmp/genesis-verify-cert.pub").Output()
	if err != nil {
		return string(signOut), fmt.Errorf("lecture du certificat généré : %w", err)
	}

	return fmt.Sprintf("PRIVATE_KEY_B64:%s\nCERTIFICATE_B64:%s",
		base64.StdEncoding.EncodeToString(priv), base64.StdEncoding.EncodeToString(cert)), nil
}

// checkSSH exécute réellement, depuis le process de test (point de vue
// « VM vérificateur »), une connexion SSH à travers l'agent Teleport (port
// node publié sur l'hôte) avec le certificat reçu -- équivalent réel de
// playbooks/check_ssh.yml.
func (f *teleportFakeAnsibleServer) checkSSH(vars map[string]any) (string, error) {
	privPEM, _ := vars["user_private_key_pem"].(string)
	certOpenSSH, _ := vars["user_certificate_openssh"].(string)
	agentUser, _ := vars["agent_user"].(string)
	if privPEM == "" || certOpenSSH == "" || agentUser == "" {
		return "", fmt.Errorf("vars user_private_key_pem/user_certificate_openssh/agent_user manquantes")
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
	return string(out), fmt.Errorf("connexion SSH via l'agent a échoué : %w", err)
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

	// step-ca réel : seul fournisseur pki.issuer/v1 crédible pour ce test
	// (même choix que internal/modulehost/vault_test.go).
	stepCABinary, stepCAManifest := buildModule(t, "step-ca")
	stepCAClient, err := modulehost.Launch(stepCABinary, stepCAManifest)
	if err != nil {
		t.Fatalf("modulehost.Launch(step-ca) : %v", err)
	}
	t.Cleanup(stepCAClient.Close)
	stepCAToken := registry.OpenSession(stepCAClient.Broker(), "step-ca", []string{"core.container/v1", "core.secrets/v1"})
	if _, err := stepCAClient.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: stepCAToken}); err != nil {
		t.Fatalf("Check(step-ca) : %v", err)
	}
	seedToken := registry.OpenSession(stepCAClient.Broker(), "step-ca", []string{"core.container/v1", "core.secrets/v1"})
	if _, err := stepCAClient.Module().SeedUp(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: seedToken}); err != nil {
		t.Fatalf("SeedUp(step-ca) : %v", err)
	}
	stepCAConn, err := stepCAClient.DispenseFunction("pki.issuer/v1")
	if err != nil {
		t.Fatalf("DispenseFunction(step-ca, pki.issuer/v1) : %v", err)
	}

	binaryPath, manifest := buildModule(t, "teleport")
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("modulehost.Launch(teleport) : %v", err)
	}
	t.Cleanup(client.Close)

	vmServer := newTeleportFakeComputeVMServer()
	ansibleServer := &teleportFakeAnsibleServer{t: t}
	t.Cleanup(ansibleServer.cleanup)

	sessionID := stepCAClient.Broker().NextId()
	_ = sessionID // NextId() n'est qu'un compteur partagé ; on en veut un côté teleport.
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
		t.Fatalf("Check(teleport) : %v", err)
	}
	provisionResp, err := client.Module().Provision(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Provision(teleport) : %v", err)
	}
	configureResp, err := client.Module().Configure(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token, State: provisionResp.GetState()})
	if err != nil {
		t.Fatalf("Configure(teleport) : %v", err)
	}
	if configureResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Configure(teleport).Status = %v", configureResp.GetStatus())
	}

	return teleportTestHandle{client: client, token: token, configured: configureResp, ansibleServer: ansibleServer}
}

// TestTeleportConfiguresAuthProxy prouve, contre un vrai conteneur
// Teleport et un vrai step-ca, le déploiement Auth+Proxy (docs07 : TLS émis
// par pki.issuer/v1, pin de la CA lu via tctl status).
func TestTeleportConfiguresAuthProxy(t *testing.T) {
	h := launchTeleportTest(t)
	if h.ansibleServer.calls < 1 {
		t.Error("install_teleport.yml n'a jamais été envoyé")
	}
}

// TestTeleportFleetAgentInstallJoinsClusterAndAllowsSSH prouve, de bout en
// bout et contre de vrais conteneurs Teleport, fleet.agent/v1.Install
// (ADR-017 : jeton d'enrôlement, jonction du cluster) puis une connexion
// SSH réelle via l'agent avec un certificat signé par la CA interne
// (ADR-018) -- exactement le scénario testé à la main dans Docker avant
// d'écrire ce module.
func TestTeleportFleetAgentInstallJoinsClusterAndAllowsSSH(t *testing.T) {
	h := launchTeleportTest(t)
	ctx := context.Background()

	verifyResp, err := h.client.Module().Verify(ctx, &modulev1.StepRequest{
		RunId: "test", BrokerToken: h.token, State: h.configured.GetState(),
	})
	if err != nil {
		t.Fatalf("Verify(teleport) : %v", err)
	}
	if verifyResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("Verify(teleport).Status = %v", verifyResp.GetStatus())
	}
	if h.ansibleServer.calls < 5 {
		t.Errorf("les playbooks de Verify n'ont pas tous été envoyés (appels ansible = %d)", h.ansibleServer.calls)
	}
}
