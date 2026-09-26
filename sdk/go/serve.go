// SPDX-License-Identifier: Apache-2.0

package sdk

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// Handshake est le handshake go-plugin partagé par le cœur (client, voir
// internal/modulehost) et chaque module (serveur, via Serve) — docs/03 §2.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "GENESIS_MODULE",
	MagicCookieValue: "genesis-module-v1",
}

// PluginKey est le nom sous lequel le plugin de cycle de vie du module est
// servi/dispensé.
const PluginKey = "module"

// FunctionPluginKey est le nom sous lequel une fonction fournie par le
// module est dispensée par le cœur (internal/broker), pour router les
// appels des autres modules — docs/02-architecture.md.
func FunctionPluginKey(function string) string {
	return "function:" + function
}

// FunctionProvider associe une fonction fournie par le module à
// l'enregistrement gRPC correspondant (ex. computevmv1.RegisterComputeVMServer).
type FunctionProvider struct {
	Name     string // ex. "compute.vm/v1"
	Register func(*grpc.Server)
}

// BrokerAware est implémentée par un module qui a besoin d'appeler d'autres
// fonctions pendant ses étapes (docs/02-architecture.md, broker de
// fonctions). SetBroker est appelée une fois au démarrage du plugin.
type BrokerAware interface {
	SetBroker(*BrokerClient)
}

// BrokerClient permet à un module de dialer le sous-canal de broker établi
// par le cœur pour l'étape en cours (StepRequest.broker_token).
type BrokerClient struct {
	broker *plugin.GRPCBroker
}

// Dial ouvre la connexion vers la session de broker désignée par token
// (StepRequest.broker_token). Un appel à une fonction non déclarée dans le
// requires du module échoue côté serveur avec codes.Unimplemented : c'est le
// refus du broker (docs/08-jalons.md, critère d'acceptation J4).
func (b *BrokerClient) Dial(token string) (*grpc.ClientConn, error) {
	id, err := strconv.ParseUint(token, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("broker_token invalide : %w", err)
	}
	return b.broker.Dial(uint32(id))
}

// modulePlugin adapte une implémentation modulev1.ModuleServer en
// plugin.GRPCPlugin.
type modulePlugin struct {
	plugin.NetRPCUnsupportedPlugin
	Impl modulev1.ModuleServer
}

func (p *modulePlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	if aware, ok := p.Impl.(BrokerAware); ok {
		aware.SetBroker(&BrokerClient{broker: broker})
	}
	modulev1.RegisterModuleServer(s, p.Impl)
	return nil
}

// ModuleConnection est ce que dispense le plugin "module" côté hôte : le
// client de cycle de vie et le broker de la connexion, dont le cœur a besoin
// pour ouvrir des sessions de broker (internal/broker, internal/modulehost).
type ModuleConnection struct {
	Client modulev1.ModuleClient
	Broker *plugin.GRPCBroker
}

func (p *modulePlugin) GRPCClient(_ context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &ModuleConnection{Client: modulev1.NewModuleClient(c), Broker: broker}, nil
}

// functionPlugin expose une fonction fournie par le module (côté serveur) ou
// la dispense comme une connexion brute (côté hôte) : le type concret du
// client est construit par l'appelant (internal/broker), qui seul connaît le
// type de la fonction nommée.
type functionPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	register func(*grpc.Server)
}

func (p *functionPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	p.register(s)
	return nil
}

func (p *functionPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return c, nil
}

// ClientPluginsFor construit la table de plugins go-plugin côté hôte pour un
// module dont le manifest est déjà connu (internal/modulehost.Launch) :
// "module" plus un "function:<nom>" par fonction fournie (docs/02).
func ClientPluginsFor(manifest *ManifestFile) map[string]plugin.Plugin {
	plugins := map[string]plugin.Plugin{PluginKey: &modulePlugin{}}
	for _, fn := range manifest.Provides {
		plugins[FunctionPluginKey(fn.Function)] = &functionPlugin{}
	}
	return plugins
}

// ClientPlugins est la table minimale (cycle de vie seulement), pour les cas
// où le manifest n'est pas encore connu (ex. premier Describe()).
func ClientPlugins() map[string]plugin.Plugin {
	return map[string]plugin.Plugin{PluginKey: &modulePlugin{}}
}

// Serve démarre le module comme plugin go-plugin : le cycle de vie (impl)
// et, pour chaque fonction fournie, un plugin "function:<nom>" que le cœur
// dispense pour router les appels des autres modules (internal/broker) —
// docs/10-ajouter-un-module.md.
func Serve(impl modulev1.ModuleServer, functions ...FunctionProvider) {
	plugins := map[string]plugin.Plugin{PluginKey: &modulePlugin{Impl: impl}}
	for _, f := range functions {
		plugins[FunctionPluginKey(f.Name)] = &functionPlugin{register: f.Register}
	}
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         plugins,
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}
