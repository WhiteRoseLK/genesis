// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc"

	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
)

type fakeDeleteClient struct {
	computevmv1.ComputeVMClient
	errs    map[string]error
	deleted []string
}

func (f *fakeDeleteClient) DeleteVM(_ context.Context, in *computevmv1.DeleteVMRequest, _ ...grpc.CallOption) (*computevmv1.DeleteVMResponse, error) {
	f.deleted = append(f.deleted, in.GetName())
	if err := f.errs[in.GetName()]; err != nil {
		return nil, err
	}
	return &computevmv1.DeleteVMResponse{}, nil
}

type fakeReleaser struct {
	errs     map[string]error
	released []string
}

func (f *fakeReleaser) Allocate(string) (string, error) { return "", nil }
func (f *fakeReleaser) Gateway() string                 { return "" }
func (f *fakeReleaser) Release(name string) error {
	f.released = append(f.released, name)
	return f.errs[name]
}

func TestDeleteVerifierVMsSuccess(t *testing.T) {
	c, a := &fakeDeleteClient{}, &fakeReleaser{}
	if err := deleteVerifierVMs(context.Background(), c, a); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.deleted) != 2 || len(a.released) != 2 {
		t.Errorf("deleted=%v released=%v, want 2 each", c.deleted, a.released)
	}
}

func TestDeleteVerifierVMsReportsErrorsAndContinues(t *testing.T) {
	c := &fakeDeleteClient{errs: map[string]error{sharedVerifierVMName: errors.New("api down")}}
	a := &fakeReleaser{errs: map[string]error{"teleport01-agent-target": errors.New("disk full")}}
	err := deleteVerifierVMs(context.Background(), c, a)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{sharedVerifierVMName, "api down", "teleport01-agent-target", "disk full"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if len(c.deleted) != 2 {
		t.Errorf("deleted=%v: the second VM must still be attempted", c.deleted)
	}
	if len(a.released) != 2 {
		t.Errorf("released=%v: both releases must still be attempted", a.released)
	}
}

func TestDeleteVerifierVMsNilAllocator(t *testing.T) {
	if err := deleteVerifierVMs(context.Background(), &fakeDeleteClient{}, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
