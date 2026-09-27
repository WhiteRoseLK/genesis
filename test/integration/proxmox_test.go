// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	computevmv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// fakeProxmoxServer simulates just enough of the Proxmox VE API to exercise
// proxmox end to end: VM list, next-id, clone, cloud-init config, start,
// status, delete, version, time (docs/08-milestones.md, M5 — no real cluster
// in this environment, see docs/PROGRESS.md).
type fakeProxmoxServer struct {
	mu       sync.Mutex
	nextID   int
	vms      map[int]*fakeVM
	template fakeVM
}

type fakeVM struct {
	VMID     int
	Name     string
	Status   string
	Template int
}

func newFakeProxmoxServer() *fakeProxmoxServer {
	return &fakeProxmoxServer{
		nextID:   100,
		vms:      map[int]*fakeVM{},
		template: fakeVM{VMID: 9000, Name: "debian-13", Template: 1},
	}
}

func (f *fakeProxmoxServer) writeData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func (f *fakeProxmoxServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		path := r.URL.Path
		switch {
		case path == "/api2/json/version":
			f.writeData(w, map[string]string{"version": "8.1.3"})

		case path == "/api2/json/nodes/pve01/time":
			f.writeData(w, map[string]int64{"time": 1234567890})

		case path == "/api2/json/cluster/nextid":
			id := f.nextID
			f.nextID++
			f.writeData(w, itoa(id))

		case path == "/api2/json/nodes/pve01/qemu" && r.Method == http.MethodGet:
			list := []map[string]any{{"vmid": f.template.VMID, "name": f.template.Name, "template": f.template.Template}}
			ids := make([]int, 0, len(f.vms))
			for id := range f.vms {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			for _, id := range ids {
				vm := f.vms[id]
				list = append(list, map[string]any{"vmid": vm.VMID, "name": vm.Name, "status": vm.Status})
			}
			f.writeData(w, list)

		case strings.HasSuffix(path, "/clone"):
			// {vmid} in the path is the SOURCE template; the new ID arrives in
			// the "newid" form field, not in the path (the real shape of the
			// Proxmox API).
			_ = r.ParseForm()
			newID, err := strconv.Atoi(r.FormValue("newid"))
			if err != nil {
				http.Error(w, `{"data":null,"errors":"missing or invalid newid"}`, http.StatusBadRequest)
				return
			}
			f.vms[newID] = &fakeVM{VMID: newID, Name: r.FormValue("name"), Status: "stopped"}
			f.writeData(w, nil)

		case strings.HasSuffix(path, "/config"):
			f.writeData(w, nil) // stopped VM: applied synchronously

		case strings.HasSuffix(path, "/status/start"):
			id := extractVMID(path, "/nodes/pve01/qemu/", "/status/start")
			if vm, ok := f.vms[id]; ok {
				vm.Status = "running"
			}
			f.writeData(w, nil)

		case strings.HasSuffix(path, "/status/current"):
			id := extractVMID(path, "/nodes/pve01/qemu/", "/status/current")
			vm, ok := f.vms[id]
			if !ok {
				http.Error(w, `{"data":null}`, http.StatusNotFound)
				return
			}
			f.writeData(w, map[string]any{"vmid": vm.VMID, "name": vm.Name, "status": vm.Status})

		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/api2/json/nodes/pve01/qemu/"):
			id := extractVMID(path, "/nodes/pve01/qemu/", "")
			delete(f.vms, id)
			f.writeData(w, nil)

		default:
			http.Error(w, `{"data":null,"errors":"unknown fixture route: `+path+`"}`, http.StatusNotImplemented)
		}
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// extractVMID extracts the numeric ID between prefix and suffix in a path such
// as /api2/json/nodes/pve01/qemu/101/clone.
func extractVMID(path, prefix, suffix string) int {
	rest := path[strings.Index(path, prefix)+len(prefix):]
	if suffix != "" {
		rest = strings.TrimSuffix(rest, suffix)
	}
	rest = strings.Split(rest, "/")[0]
	id, err := strconv.Atoi(rest)
	if err != nil {
		return 0
	}
	return id
}

func stepRequestWithConfig(t *testing.T, cfg map[string]any) *modulev1.StepRequest {
	t.Helper()
	s, err := structpb.NewStruct(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return &modulev1.StepRequest{RunId: "test", Config: s}
}

// TestProxmoxEnsureVMIsIdempotentAndLifecycleWorks is the M5 acceptance
// criterion (doc 08): "compute spec + one placed VM -> VM created... ; re-run
// -> 0 changes; destroy deletes it" — checked against HTTP fixtures faithful
// to the documented Proxmox API, not a real cluster.
func TestProxmoxEnsureVMIsIdempotentAndLifecycleWorks(t *testing.T) {
	fake := newFakeProxmoxServer()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	binaryPath, manifest := buildModule(t, "proxmox")
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	cfg := map[string]any{
		"endpoint": srv.URL,
		"node":     "pve01",
		"image":    "debian-13",
		"credentials": map[string]any{
			"token_id":     "genesis@pve!token",
			"token_secret": "secret",
		},
	}

	checkResp, err := client.Module().Check(ctx, stepRequestWithConfig(t, cfg))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_COMPLIANT {
		t.Fatalf("Check().Status = %v, want COMPLIANT (connection to the fixture API)", checkResp.GetStatus())
	}

	conn, err := client.DispenseFunction("compute.vm/v1")
	if err != nil {
		t.Fatalf("DispenseFunction: %v", err)
	}
	vmClient := computevmv1.NewComputeVMClient(conn)

	if _, err := vmClient.EnsureImage(ctx, &computevmv1.EnsureImageRequest{Image: "debian-13"}); err != nil {
		t.Fatalf("EnsureImage: %v", err)
	}

	first, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: "infra01", Env: "lab", Ip: "10.10.0.55/24", Gateway: "10.10.0.1", User: "genesis",
	})
	if err != nil {
		t.Fatalf("EnsureVM (first time): %v", err)
	}
	if first.GetStatus() != "running" {
		t.Errorf("VM.Status = %q, want running after the start", first.GetStatus())
	}

	// Re-run -> 0 changes: the second EnsureVM must find the same VM again
	// (idempotence by name, docs/07) without creating a new one.
	second, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{Name: "infra01", Env: "lab"})
	if err != nil {
		t.Fatalf("EnsureVM (second time): %v", err)
	}
	if second.GetId() != first.GetId() {
		t.Errorf("EnsureVM created a new VM instead of finding infra01 again: %s then %s", first.GetId(), second.GetId())
	}

	got, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: "infra01"})
	if err != nil {
		t.Fatalf("GetVM: %v", err)
	}
	if got.GetId() != first.GetId() {
		t.Errorf("GetVM = %+v, want id=%s", got, first.GetId())
	}
	// Consumers (chrony, powerdns, teleport…) connect over SSH on VM.ssh_port:
	// 0 would produce `ansible_port=0` on a real VM.
	for _, vm := range []*computevmv1.VM{first, second, got} {
		if vm.GetSshPort() != 22 {
			t.Errorf("VM %s: ssh_port = %d, want 22", vm.GetId(), vm.GetSshPort())
		}
	}

	now, err := vmClient.Now(ctx, &computevmv1.NowRequest{})
	if err != nil {
		t.Fatalf("Now: %v", err)
	}
	if now.GetTime().AsTime().Unix() != 1234567890 {
		t.Errorf("Now = %v, want 1234567890", now.GetTime().AsTime().Unix())
	}

	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: "infra01"}); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if _, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: "infra01"}); err == nil {
		t.Error("GetVM after DeleteVM: unexpected success")
	}
	// DeleteVM is idempotent: a second deletion is not an error.
	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: "infra01"}); err != nil {
		t.Errorf("second DeleteVM (already deleted): unexpected error: %v", err)
	}
}
