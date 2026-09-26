// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"

	"google.golang.org/grpc"

	"github.com/WhiteRoseLK/genesis/internal/runner"
	containerv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/container/v1"
)

// NativeContainer construit le fournisseur core.container/v1, natif au cœur
// (docs/02-architecture.md : "les runners sont exposés aux modules comme
// fonctions intégrées").
func NativeContainer(rt *runner.ContainerRuntime) nativeFactory {
	return func(_ string) func(*grpc.Server) {
		return func(s *grpc.Server) {
			containerv1.RegisterContainerServer(s, &containerServer{runtime: rt})
		}
	}
}

type containerServer struct {
	containerv1.UnimplementedContainerServer
	runtime *runner.ContainerRuntime
}

func (c *containerServer) Run(ctx context.Context, req *containerv1.RunRequest) (*containerv1.RunResponse, error) {
	mounts := make([]runner.Mount, len(req.GetMounts()))
	for i, m := range req.GetMounts() {
		mounts[i] = runner.Mount{HostPath: m.GetHostPath(), ContainerPath: m.GetContainerPath(), ReadOnly: m.GetReadOnly()}
	}
	result, err := c.runtime.Run(ctx, runner.RunOptions{
		Name:    req.GetName(),
		Image:   req.GetImage(),
		Command: req.GetCommand(),
		Env:     req.GetEnv(),
		Mounts:  mounts,
		Detach:  req.GetDetach(),
	})
	if err != nil {
		return nil, err
	}

	var ip string
	if req.GetDetach() {
		// L'IP n'a de sens que pour un service qui continue de tourner ;
		// une erreur ici (réseau pas encore attribué) ne doit pas faire
		// échouer tout Run, le conteneur est bel et bien démarré.
		ip, _ = c.runtime.InspectIP(ctx, result.ContainerID)
	}

	return &containerv1.RunResponse{
		ContainerId: result.ContainerID,
		ExitCode:    int32(result.ExitCode),
		Stdout:      result.Stdout,
		Stderr:      result.Stderr,
		Ip:          ip,
	}, nil
}

func (c *containerServer) Stop(ctx context.Context, req *containerv1.StopRequest) (*containerv1.StopResponse, error) {
	if err := c.runtime.Stop(ctx, req.GetContainerId()); err != nil {
		return nil, err
	}
	return &containerv1.StopResponse{}, nil
}

func (c *containerServer) Status(ctx context.Context, req *containerv1.StatusRequest) (*containerv1.StatusResponse, error) {
	state, exitCode, err := c.runtime.Status(ctx, req.GetContainerId())
	if err != nil {
		return nil, err
	}
	return &containerv1.StatusResponse{State: state, ExitCode: int32(exitCode)}, nil
}
