// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	sdk "genesis/sdk/go"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

// Client est une connexion vivante à un module lancé en process séparé.
type Client struct {
	plugin    *goplugin.Client
	rpcClient goplugin.ClientProtocol
	module    modulev1.ModuleClient
	broker    *goplugin.GRPCBroker
}

// Launch démarre le binaire du module et établit la connexion gRPC
// (docs/02-architecture.md : hôte de modules, go-plugin). manifest peut être
// nil si les fonctions fournies par le module n'ont pas encore besoin d'être
// dispensées (ex. simple Describe() de découverte) ; il doit être fourni dès
// qu'une fonction fournie sera routée par le broker.
func Launch(binaryPath string, manifest *sdk.ManifestFile) (*Client, error) {
	plugins := sdk.ClientPlugins()
	if manifest != nil {
		plugins = sdk.ClientPluginsFor(manifest)
	}

	pc := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  sdk.Handshake,
		Plugins:          plugins,
		Cmd:              exec.Command(binaryPath),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		// Par défaut go-plugin journalise en DEBUG/TRACE sur stderr ; Warn
		// évite de noyer la sortie du cœur avec le détail du transport gRPC.
		Logger: hclog.New(&hclog.LoggerOptions{Name: "modulehost", Level: hclog.Warn}),
	})

	rpcClient, err := pc.Client()
	if err != nil {
		pc.Kill()
		return nil, fmt.Errorf("démarrage du module %s : %w", binaryPath, err)
	}
	raw, err := rpcClient.Dispense(sdk.PluginKey)
	if err != nil {
		pc.Kill()
		return nil, fmt.Errorf("connexion au module %s : %w", binaryPath, err)
	}
	conn, ok := raw.(*sdk.ModuleConnection)
	if !ok {
		pc.Kill()
		return nil, fmt.Errorf("module %s : type de client inattendu (%T)", binaryPath, raw)
	}
	return &Client{plugin: pc, rpcClient: rpcClient, module: conn.Client, broker: conn.Broker}, nil
}

// Close arrête le process du module. Un module qui a déjà planté n'entraîne
// pas d'erreur ici : Kill est idempotent côté go-plugin.
func (c *Client) Close() {
	c.plugin.Kill()
}

// Module donne accès au client gRPC brut, pour appeler n'importe quelle
// étape du cycle de vie (docs/03-contrat-module.md §2).
func (c *Client) Module() modulev1.ModuleClient {
	return c.module
}

// Broker donne accès au canal bidirectionnel go-plugin de cette connexion,
// pour ouvrir une session de broker avant d'invoquer une étape
// (internal/broker, docs/02-architecture.md).
func (c *Client) Broker() *goplugin.GRPCBroker {
	return c.broker
}

// DispenseFunction ouvre la connexion brute vers une fonction fournie par ce
// module (déclarée dans son manifest.Provides), à typer par l'appelant
// (internal/broker, qui seul connaît le type concret de la fonction).
func (c *Client) DispenseFunction(name string) (*grpc.ClientConn, error) {
	raw, err := c.rpcClient.Dispense(sdk.FunctionPluginKey(name))
	if err != nil {
		return nil, fmt.Errorf("connexion à la fonction %q : %w", name, err)
	}
	conn, ok := raw.(*grpc.ClientConn)
	if !ok {
		return nil, fmt.Errorf("fonction %q : type de connexion inattendu (%T)", name, raw)
	}
	return conn, nil
}

// Describe interroge le manifest publié par le module en cours d'exécution.
func (c *Client) Describe(ctx context.Context) (*modulev1.Manifest, error) {
	m, err := c.module.Describe(ctx, &modulev1.Empty{})
	if err != nil {
		return nil, WrapModuleError("Describe", err)
	}
	return m, nil
}

// WrapModuleError transforme une erreur de transport gRPC (un module qui a
// planté ou n'a pas répondu pendant une étape) en erreur actionnable, sans
// jamais faire s'arrêter le cœur (critère d'acceptation du jalon J3, doc 08).
func WrapModuleError(step string, err error) error {
	return fmt.Errorf("module : étape %q en échec (le module a peut-être planté, le cœur continue) : %w", step, err)
}
