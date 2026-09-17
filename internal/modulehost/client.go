// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"

	sdk "genesis/sdk/go"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

// Client est une connexion vivante à un module lancé en process séparé.
type Client struct {
	plugin *goplugin.Client
	module modulev1.ModuleClient
}

// Launch démarre le binaire du module et établit la connexion gRPC
// (docs/02-architecture.md : hôte de modules, go-plugin).
func Launch(binaryPath string) (*Client, error) {
	pc := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  sdk.Handshake,
		Plugins:          sdk.ClientPlugins(),
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
	moduleClient, ok := raw.(modulev1.ModuleClient)
	if !ok {
		pc.Kill()
		return nil, fmt.Errorf("module %s : type de client inattendu (%T)", binaryPath, raw)
	}
	return &Client{plugin: pc, module: moduleClient}, nil
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
