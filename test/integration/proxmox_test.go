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

// fakeProxmoxServer simule juste assez de l'API Proxmox VE pour exercer
// proxmox de bout en bout : liste de VM, next-id, clone, config cloud-init,
// start, status, delete, version, time (docs/08-milestones.md, J5 — pas de
// cluster réel dans cet environnement, voir docs/PROGRESS.md).
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
			// {vmid} dans le chemin est le template SOURCE ; le nouvel
			// identifiant arrive dans le champ de formulaire "newid", pas
			// dans le chemin (vraie forme de l'API Proxmox).
			_ = r.ParseForm()
			newID, err := strconv.Atoi(r.FormValue("newid"))
			if err != nil {
				http.Error(w, `{"data":null,"errors":"newid manquant ou invalide"}`, http.StatusBadRequest)
				return
			}
			f.vms[newID] = &fakeVM{VMID: newID, Name: r.FormValue("name"), Status: "stopped"}
			f.writeData(w, nil)

		case strings.HasSuffix(path, "/config"):
			f.writeData(w, nil) // VM arrêtée : appliqué de façon synchrone

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
			http.Error(w, `{"data":null,"errors":"route de fixture inconnue: `+path+`"}`, http.StatusNotImplemented)
		}
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// extractVMID extrait l'identifiant numérique entre prefix et suffix dans
// un chemin comme /api2/json/nodes/pve01/qemu/101/clone.
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

// TestProxmoxEnsureVMIsIdempotentAndLifecycleWorks est le critère
// d'acceptation du jalon J5 (doc 08) : "spec compute + une VM placée -> VM
// créée... ; relance -> 0 changement ; destroy la supprime" — vérifié contre
// des fixtures HTTP fidèles à l'API Proxmox documentée, pas un vrai cluster.
func TestProxmoxEnsureVMIsIdempotentAndLifecycleWorks(t *testing.T) {
	fake := newFakeProxmoxServer()
	srv := httptest.NewServer(fake.handler())
	t.Cleanup(srv.Close)

	binaryPath, manifest := buildModule(t, "proxmox")
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch : %v", err)
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
		t.Fatalf("Check : %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_CONFORME {
		t.Fatalf("Check().Status = %v, attendu CONFORME (connexion à l'API de fixtures)", checkResp.GetStatus())
	}

	conn, err := client.DispenseFunction("compute.vm/v1")
	if err != nil {
		t.Fatalf("DispenseFunction : %v", err)
	}
	vmClient := computevmv1.NewComputeVMClient(conn)

	if _, err := vmClient.EnsureImage(ctx, &computevmv1.EnsureImageRequest{Image: "debian-13"}); err != nil {
		t.Fatalf("EnsureImage : %v", err)
	}

	first, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{
		Name: "infra01", Env: "lab", Ip: "10.10.0.55/24", Gateway: "10.10.0.1", User: "genesis",
	})
	if err != nil {
		t.Fatalf("EnsureVM (première fois) : %v", err)
	}
	if first.GetStatus() != "running" {
		t.Errorf("VM.Status = %q, attendu running après le démarrage", first.GetStatus())
	}

	// Relance -> 0 changement : la seconde EnsureVM doit retrouver la même
	// VM (idempotence par nom, docs/07) sans en créer une nouvelle.
	second, err := vmClient.EnsureVM(ctx, &computevmv1.EnsureVMRequest{Name: "infra01", Env: "lab"})
	if err != nil {
		t.Fatalf("EnsureVM (seconde fois) : %v", err)
	}
	if second.GetId() != first.GetId() {
		t.Errorf("EnsureVM a créé une nouvelle VM au lieu de retrouver infra01 : %s puis %s", first.GetId(), second.GetId())
	}

	got, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: "infra01"})
	if err != nil {
		t.Fatalf("GetVM : %v", err)
	}
	if got.GetId() != first.GetId() {
		t.Errorf("GetVM = %+v, attendu id=%s", got, first.GetId())
	}
	// Les consommateurs (chrony, powerdns, teleport…) se connectent en SSH sur
	// VM.ssh_port : 0 produirait `ansible_port=0` sur une vraie VM.
	for _, vm := range []*computevmv1.VM{first, second, got} {
		if vm.GetSshPort() != 22 {
			t.Errorf("VM %s : ssh_port = %d, attendu 22", vm.GetId(), vm.GetSshPort())
		}
	}

	now, err := vmClient.Now(ctx, &computevmv1.NowRequest{})
	if err != nil {
		t.Fatalf("Now : %v", err)
	}
	if now.GetTime().AsTime().Unix() != 1234567890 {
		t.Errorf("Now = %v, attendu 1234567890", now.GetTime().AsTime().Unix())
	}

	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: "infra01"}); err != nil {
		t.Fatalf("DeleteVM : %v", err)
	}
	if _, err := vmClient.GetVM(ctx, &computevmv1.GetVMRequest{Name: "infra01"}); err == nil {
		t.Error("GetVM après DeleteVM : succès inattendu")
	}
	// DeleteVM est idempotent : une seconde suppression n'est pas une erreur.
	if _, err := vmClient.DeleteVM(ctx, &computevmv1.DeleteVMRequest{Name: "infra01"}); err != nil {
		t.Errorf("second DeleteVM (déjà supprimée) : erreur inattendue : %v", err)
	}
}
