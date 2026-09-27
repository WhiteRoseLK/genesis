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

// Handshake is the go-plugin handshake shared by the core (client, see
// internal/modulehost) and every module (server, through Serve) — docs/03 §2.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "GENESIS_MODULE",
	MagicCookieValue: "genesis-module-v1",
}

// PluginKey is the name under which the module's lifecycle plugin is
// served/dispensed.
const PluginKey = "module"

// FunctionPluginKey is the name under which a function provided by the module
// is dispensed by the core (internal/broker), to route calls from other
// modules — docs/02-architecture.md.
func FunctionPluginKey(function string) string {
	return "function:" + function
}

// FunctionProvider associates a function provided by the module with the
// matching gRPC registration (e.g. computevmv1.RegisterComputeVMServer).
type FunctionProvider struct {
	Name     string // ex. "compute.vm/v1"
	Register func(*grpc.Server)
}

// BrokerAware is implemented by a module that needs to call other functions
// during its steps (docs/02-architecture.md, function broker). SetBroker is
// called once when the plugin starts.
type BrokerAware interface {
	SetBroker(*BrokerClient)
}

// BrokerClient lets a module dial the broker subchannel set up by the core for
// the current step (StepRequest.broker_token).
type BrokerClient struct {
	broker *plugin.GRPCBroker
}

// Dial opens the connection to the broker session designated by token
// (StepRequest.broker_token). A call to a function not declared in the
// module's requires fails on the server side with codes.Unimplemented: that is
// the broker's refusal (docs/08-milestones.md, M4 acceptance criterion).
func (b *BrokerClient) Dial(token string) (*grpc.ClientConn, error) {
	id, err := strconv.ParseUint(token, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid broker_token: %w", err)
	}
	return b.broker.Dial(uint32(id))
}

// modulePlugin adapts a modulev1.ModuleServer implementation into a
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

// ModuleConnection is what the "module" plugin dispenses on the host side: the
// lifecycle client and the connection's broker, which the core needs to open
// broker sessions (internal/broker, internal/modulehost).
type ModuleConnection struct {
	Client modulev1.ModuleClient
	Broker *plugin.GRPCBroker
}

func (p *modulePlugin) GRPCClient(_ context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return &ModuleConnection{Client: modulev1.NewModuleClient(c), Broker: broker}, nil
}

// functionPlugin exposes a function provided by the module (server side) or
// dispenses it as a raw connection (host side): the concrete client type is
// built by the caller (internal/broker), which alone knows the type of the
// named function.
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

// ClientPluginsFor builds the host-side go-plugin plugin table for a module
// whose manifest is already known (internal/modulehost.Launch): "module" plus
// one "function:<name>" per provided function (docs/02).
func ClientPluginsFor(manifest *ManifestFile) map[string]plugin.Plugin {
	plugins := map[string]plugin.Plugin{PluginKey: &modulePlugin{}}
	for _, fn := range manifest.Provides {
		plugins[FunctionPluginKey(fn.Function)] = &functionPlugin{}
	}
	return plugins
}

// ClientPlugins is the minimal table (lifecycle only), for cases where the
// manifest is not known yet (e.g. the first Describe()).
func ClientPlugins() map[string]plugin.Plugin {
	return map[string]plugin.Plugin{PluginKey: &modulePlugin{}}
}

// Serve starts the module as a go-plugin plugin: the lifecycle (impl) and, for
// each provided function, a "function:<name>" plugin that the core dispenses
// to route calls from other modules (internal/broker) —
// docs/10-adding-a-module.md.
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
