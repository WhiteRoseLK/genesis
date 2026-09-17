// SPDX-License-Identifier: Apache-2.0

package sdk

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	modulev1 "genesis/sdk/go/gen/module/v1"
)

// Handshake est le handshake go-plugin partagé par le cœur (client, voir
// internal/modulehost) et chaque module (serveur, via Serve) — docs/03 §2.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "GENESIS_MODULE",
	MagicCookieValue: "genesis-module-v1",
}

// PluginKey est le nom sous lequel le plugin module est servi/dispensé.
const PluginKey = "module"

// grpcPlugin adapte une implémentation modulev1.ModuleServer en
// plugin.GRPCPlugin. Impl est vide côté client (internal/modulehost) : seul
// GRPCClient y est appelé.
type grpcPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	Impl modulev1.ModuleServer
}

func (p *grpcPlugin) GRPCServer(_ *plugin.GRPCBroker, s *grpc.Server) error {
	modulev1.RegisterModuleServer(s, p.Impl)
	return nil
}

func (p *grpcPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return modulev1.NewModuleClient(c), nil
}

// ClientPlugins est la table de plugins go-plugin côté hôte (cœur) : elle
// n'a besoin d'aucune implémentation, seul GRPCClient est utilisé.
func ClientPlugins() map[string]plugin.Plugin {
	return map[string]plugin.Plugin{PluginKey: &grpcPlugin{}}
}

// Serve démarre le module comme plugin go-plugin ; à appeler depuis main()
// d'un module (docs/10-ajouter-un-module.md).
func Serve(impl modulev1.ModuleServer) {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         map[string]plugin.Plugin{PluginKey: &grpcPlugin{Impl: impl}},
		GRPCServer:      plugin.DefaultGRPCServer,
	})
}
