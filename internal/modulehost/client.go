// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// Client is a live connection to a module launched as a separate process.
type Client struct {
	plugin    *goplugin.Client
	rpcClient goplugin.ClientProtocol
	module    modulev1.ModuleClient
	broker    *goplugin.GRPCBroker
}

// Launch starts the module binary and sets up the gRPC connection
// (docs/02-architecture.md: module host, go-plugin). manifest may be nil if
// the functions provided by the module do not need to be dispensed yet (e.g. a
// simple discovery Describe()); it must be provided as soon as a provided
// function is to be routed by the broker.
func Launch(binaryPath string, manifest *sdk.ManifestFile) (*Client, error) {
	plugins := sdk.ClientPlugins()
	if manifest != nil {
		plugins = sdk.ClientPluginsFor(manifest)
	}

	pc := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: sdk.Handshake,
		Plugins:         plugins,
		// No context: go-plugin manages the process lifetime (Client.Close
		// ends it), which outlives the call to Launch.
		Cmd:              exec.Command(binaryPath), //nolint:noctx // see above
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		// By default go-plugin logs at DEBUG/TRACE on stderr; Warn avoids
		// drowning the core's output in gRPC transport details.
		Logger: hclog.New(&hclog.LoggerOptions{Name: "modulehost", Level: hclog.Warn}),
	})

	rpcClient, err := pc.Client()
	if err != nil {
		pc.Kill()
		return nil, fmt.Errorf("starting module %s: %w", binaryPath, err)
	}
	raw, err := rpcClient.Dispense(sdk.PluginKey)
	if err != nil {
		pc.Kill()
		return nil, fmt.Errorf("connecting to module %s: %w", binaryPath, err)
	}
	conn, ok := raw.(*sdk.ModuleConnection)
	if !ok {
		pc.Kill()
		return nil, fmt.Errorf("module %s: unexpected client type (%T)", binaryPath, raw)
	}
	return &Client{plugin: pc, rpcClient: rpcClient, module: conn.Client, broker: conn.Broker}, nil
}

// Close stops the module process. A module that has already crashed causes no
// error here: Kill is idempotent on the go-plugin side.
func (c *Client) Close() {
	c.plugin.Kill()
}

// Module gives access to the raw gRPC client, to call any lifecycle step
// (docs/03-module-contract.md §2).
func (c *Client) Module() modulev1.ModuleClient {
	return c.module
}

// Broker gives access to this connection's bidirectional go-plugin channel, to
// open a broker session before invoking a step (internal/broker,
// docs/02-architecture.md).
func (c *Client) Broker() *goplugin.GRPCBroker {
	return c.broker
}

// DispenseFunction opens the raw connection to a function provided by this
// module (declared in its manifest.Provides), to be typed by the caller
// (internal/broker, which alone knows the function's concrete type).
func (c *Client) DispenseFunction(name string) (*grpc.ClientConn, error) {
	raw, err := c.rpcClient.Dispense(sdk.FunctionPluginKey(name))
	if err != nil {
		return nil, fmt.Errorf("connecting to function %q: %w", name, err)
	}
	conn, ok := raw.(*grpc.ClientConn)
	if !ok {
		return nil, fmt.Errorf("function %q: unexpected connection type (%T)", name, raw)
	}
	return conn, nil
}

// Describe queries the manifest published by the running module.
func (c *Client) Describe(ctx context.Context) (*modulev1.Manifest, error) {
	m, err := c.module.Describe(ctx, &modulev1.Empty{})
	if err != nil {
		return nil, WrapModuleError("Describe", err)
	}
	return m, nil
}

// WrapModuleError turns a gRPC transport error (a module that crashed or did
// not answer during a step) into an actionable error, without ever stopping
// the core (M3 acceptance criterion, doc 08).
func WrapModuleError(step string, err error) error {
	return fmt.Errorf("module: step %q failed (the module may have crashed, the core keeps running): %w", step, err)
}
