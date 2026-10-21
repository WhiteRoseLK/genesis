// SPDX-License-Identifier: Apache-2.0

package proxmoxapi

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer builds a fixture server: handler maps method+path to a
// function that writes the JSON response (already wrapped in {"data": ...} by
// writeData, or an error through writeError).
func newTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	client := New(srv.URL, "genesis@pve!token", "secret-uuid", srv.Client())
	return client, srv
}

func writeData(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizationHeader(t *testing.T) {
	var gotAuth string
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeData(t, w, map[string]string{"version": "8.1.3"})
	})

	if _, err := client.Version(context.Background()); err != nil {
		t.Fatalf("Version: %v", err)
	}
	want := "PVEAPIToken=genesis@pve!token=secret-uuid"
	if gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestVersionUsesAPIPrefix(t *testing.T) {
	var gotPath string
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeData(t, w, map[string]string{"version": "8.1.3"})
	})
	if _, err := client.Version(context.Background()); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api2/json/version" {
		t.Errorf("path = %q, want /api2/json/version", gotPath)
	}
}

func TestNonSuccessStatusReturnsError(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"data":null,"errors":{"token":"invalid"}}`))
	})
	_, err := client.Version(context.Background())
	if err == nil {
		t.Fatal("403 response: unexpected success")
	}
}

func TestWaitForTaskPollsUntilStopped(t *testing.T) {
	calls := 0
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/tasks/") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		calls++
		if calls < 3 {
			writeData(t, w, TaskStatus{Status: "running"})
			return
		}
		writeData(t, w, TaskStatus{Status: "stopped", ExitStatus: "OK"})
	})

	if err := client.WaitForTask(context.Background(), "pve01", "UPID:pve01:test"); err != nil {
		t.Fatalf("WaitForTask: %v", err)
	}
	if calls != 3 {
		t.Errorf("calls = %d, want 3 polls before stopped", calls)
	}
}

func TestWaitForTaskReturnsErrorOnFailedTask(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeData(t, w, TaskStatus{Status: "stopped", ExitStatus: "simulated error"})
	})
	err := client.WaitForTask(context.Background(), "pve01", "UPID:pve01:test")
	if err == nil {
		t.Fatal("failed task: unexpected success")
	}
}

func TestFindVMByNameSkipsTemplates(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeData(t, w, []VM{
			{VMID: 9000, Name: "debian-13", Template: 1},
			{VMID: 100, Name: "infra01", Status: "running"},
		})
	})
	vm, err := client.FindVMByName(context.Background(), "pve01", "infra01")
	if err != nil {
		t.Fatalf("FindVMByName: %v", err)
	}
	if vm == nil || vm.VMID != 100 {
		t.Errorf("FindVMByName = %+v, want vmid=100", vm)
	}

	notFound, err := client.FindVMByName(context.Background(), "pve01", "debian-13")
	if err != nil {
		t.Fatal(err)
	}
	if notFound != nil {
		t.Errorf("FindVMByName(debian-13) = %+v, should not return the template", notFound)
	}
}

func TestFindTemplateByName(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeData(t, w, []VM{
			{VMID: 9000, Name: "debian-13", Template: 1},
			{VMID: 100, Name: "infra01", Status: "running"},
		})
	})
	tmpl, err := client.FindTemplateByName(context.Background(), "pve01", "debian-13")
	if err != nil {
		t.Fatalf("FindTemplateByName: %v", err)
	}
	if tmpl == nil || tmpl.VMID != 9000 {
		t.Errorf("FindTemplateByName = %+v, want vmid=9000", tmpl)
	}
}

func TestCloneVMWaitsForAsyncTask(t *testing.T) {
	var gotForm string
	taskCalls := 0
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/clone"):
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			gotForm = string(body)
			writeData(t, w, "UPID:pve01:clone-task")
		case strings.Contains(r.URL.Path, "/tasks/"):
			taskCalls++
			writeData(t, w, TaskStatus{Status: "stopped", ExitStatus: "OK"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})

	err := client.CloneVM(context.Background(), "pve01", CloneVMOptions{TemplateID: 9000, NewID: 101, Name: "infra01"})
	if err != nil {
		t.Fatalf("CloneVM: %v", err)
	}
	if taskCalls == 0 {
		t.Error("CloneVM never polled the asynchronous task")
	}
	if !strings.Contains(gotForm, "newid=101") || !strings.Contains(gotForm, "full=0") {
		t.Errorf("form sent = %q, want newid=101 and full=0 (linked clone)", gotForm)
	}
}

func TestConfigureCloudInitHandlesSyncResponse(t *testing.T) {
	// Stopped VM: Proxmox applies the config synchronously, data=null.
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeData(t, w, nil)
	})
	err := client.ConfigureCloudInit(context.Background(), "pve01", 101, CloudInitOptions{
		User: "genesis", IP: "10.10.0.55/24", Gateway: "10.10.0.1",
	})
	if err != nil {
		t.Fatalf("ConfigureCloudInit (synchronous response): %v", err)
	}
}

func TestDeleteVM(t *testing.T) {
	var gotMethod string
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		writeData(t, w, nil)
	})
	if err := client.DeleteVM(context.Background(), "pve01", 101); err != nil {
		t.Fatalf("DeleteVM: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
}

func TestGetVMConfig(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api2/json/nodes/pve01/qemu/3000/config" {
			writeData(t, w, map[string]any{"description": "Genesis Base Template", "name": "genesis-template"})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	})
	cfg, err := client.GetVMConfig(context.Background(), "pve01", 3000)
	if err != nil {
		t.Fatalf("GetVMConfig: %v", err)
	}
	if cfg["description"] != "Genesis Base Template" {
		t.Errorf("description = %v, want Genesis Base Template", cfg["description"])
	}
}

func TestNewHTTPClient(t *testing.T) {
	t.Run("default options return DefaultClient", func(t *testing.T) {
		hc, err := NewHTTPClient(TLSOptions{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hc != http.DefaultClient {
			t.Errorf("got %v, want http.DefaultClient", hc)
		}
	})

	t.Run("invalid CA cert PEM returns error", func(t *testing.T) {
		_, err := NewHTTPClient(TLSOptions{CACertPEM: []byte("not a valid pem certificate")})
		if err == nil {
			t.Fatal("expected error for invalid PEM, got nil")
		}
	})

	t.Run("insecure skip verify connects to TLS server", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeData(t, w, map[string]string{"version": "8.1.3"})
		}))
		t.Cleanup(srv.Close)

		// Without Insecure, default client fails certificate verification
		defaultClient := New(srv.URL, "genesis@pve!token", "secret", nil)
		if _, err := defaultClient.Version(context.Background()); err == nil {
			t.Fatal("expected certificate verification failure with default client, got nil")
		}

		// With Insecure: true, client succeeds
		insecureHC, err := NewHTTPClient(TLSOptions{Insecure: true})
		if err != nil {
			t.Fatalf("NewHTTPClient: %v", err)
		}
		insecureClient := New(srv.URL, "genesis@pve!token", "secret", insecureHC)
		v, err := insecureClient.Version(context.Background())
		if err != nil {
			t.Fatalf("Version with Insecure: true failed: %v", err)
		}
		if v != "8.1.3" {
			t.Errorf("Version = %q, want 8.1.3", v)
		}
	})

	t.Run("custom CA certificate connects to TLS server", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeData(t, w, map[string]string{"version": "8.1.3"})
		}))
		t.Cleanup(srv.Close)

		caPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: srv.Certificate().Raw,
		})

		caHC, err := NewHTTPClient(TLSOptions{CACertPEM: caPEM})
		if err != nil {
			t.Fatalf("NewHTTPClient: %v", err)
		}
		caClient := New(srv.URL, "genesis@pve!token", "secret", caHC)
		v, err := caClient.Version(context.Background())
		if err != nil {
			t.Fatalf("Version with custom CA failed: %v", err)
		}
		if v != "8.1.3" {
			t.Errorf("Version = %q, want 8.1.3", v)
		}
	})
}

func TestNextIDWithMinID(t *testing.T) {
	var gotQuery string
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		writeData(t, w, "3005")
	})

	id, err := client.NextID(context.Background(), 3000)
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	if id != 3005 {
		t.Errorf("NextID = %d, want 3005", id)
	}
	if gotQuery != "vmid=3000" {
		t.Errorf("query = %q, want vmid=3000", gotQuery)
	}
}

func TestCloneVMFullAndStorageOptions(t *testing.T) {
	var gotForm string
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotForm = string(body)
		writeData(t, w, nil)
	})

	err := client.CloneVM(context.Background(), "pve01", CloneVMOptions{
		TemplateID: 9000,
		NewID:      3001,
		Name:       "infra01",
		Full:       true,
		Storage:    "SSD",
	})
	if err != nil {
		t.Fatalf("CloneVM: %v", err)
	}
	if !strings.Contains(gotForm, "full=1") || !strings.Contains(gotForm, "storage=SSD") || !strings.Contains(gotForm, "newid=3001") {
		t.Errorf("form sent = %q, want full=1, storage=SSD, newid=3001", gotForm)
	}
}

func TestCloneVMFallbackToFullClone(t *testing.T) {
	attempts := 0
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		form := string(body)
		if strings.Contains(form, "full=0") {
			http.Error(w, `{"data":null,"message":"Linked clone feature is not supported for 'SSD:vm-3000-disk-0'"}`, http.StatusBadRequest)
			return
		}
		writeData(t, w, nil)
	})

	err := client.CloneVM(context.Background(), "pve01", CloneVMOptions{
		TemplateID: 9000,
		NewID:      3001,
		Name:       "infra01",
		Full:       false,
	})
	if err != nil {
		t.Fatalf("CloneVM fallback: %v", err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2 (linked clone then full clone fallback)", attempts)
	}
}

func TestPingAgent(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api2/json/nodes/pve01/qemu/3000/agent/ping" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeData(t, w, nil)
	})

	if err := client.PingAgent(context.Background(), "pve01", 3000); err != nil {
		t.Fatalf("PingAgent: %v", err)
	}
}

func TestWaitForAgent(t *testing.T) {
	calls := 0
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			http.Error(w, `{"data":null,"message":"QEMU guest agent is not running"}`, http.StatusInternalServerError)
			return
		}
		writeData(t, w, nil)
	})

	if err := client.WaitForAgent(context.Background(), "pve01", 3000); err != nil {
		t.Fatalf("WaitForAgent: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 polls before success", calls)
	}
}

func TestAgentExec(t *testing.T) {
	var gotCommand []string
	execChecked := 0
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/nodes/pve01/qemu/3000/agent/exec" {
			var body struct {
				Command []string `json:"command"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode exec body: %v", err)
			}
			gotCommand = body.Command
			writeData(t, w, map[string]int{"pid": 1234})
			return
		}
		if r.URL.Path == "/api2/json/nodes/pve01/qemu/3000/agent/exec-status" {
			execChecked++
			if execChecked < 2 {
				writeData(t, w, AgentExecResult{Exited: 0})
				return
			}
			writeData(t, w, AgentExecResult{Exited: 1, ExitCode: 0, OutData: "success"})
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	})

	res, err := client.AgentExec(context.Background(), "pve01", 3000, []string{"sh", "-c", "echo hello"})
	if err != nil {
		t.Fatalf("AgentExec: %v", err)
	}
	if res.OutData != "success" {
		t.Errorf("OutData = %q, want 'success'", res.OutData)
	}
	if len(gotCommand) != 3 || gotCommand[2] != "echo hello" {
		t.Errorf("command sent = %v, want [sh -c 'echo hello']", gotCommand)
	}
}

func TestWaitForStopped(t *testing.T) {
	calls := 0
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			writeData(t, w, VMStatus{VMID: 3000, Status: "running"})
			return
		}
		writeData(t, w, VMStatus{VMID: 3000, Status: "stopped"})
	})

	if err := client.WaitForStopped(context.Background(), "pve01", 3000); err != nil {
		t.Fatalf("WaitForStopped: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 polls before stopped", calls)
	}
}

func TestEnsureStorageContent(t *testing.T) {
	putCalled := false
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/api2/json/storage/local" {
			writeData(t, w, StorageConfig{Storage: "local", Type: "dir", Content: "images,iso"})
			return
		}
		if r.Method == http.MethodPut && r.URL.Path == "/api2/json/storage/local" {
			putCalled = true
			writeData(t, w, nil)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	})

	if err := client.EnsureStorageContent(context.Background(), "local", "import"); err != nil {
		t.Fatalf("EnsureStorageContent: %v", err)
	}
	if !putCalled {
		t.Error("expected PUT /storage/local to be called")
	}
}

func TestListAndFindStorageVolume(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/nodes/pve01/storage/local/content" {
			writeData(t, w, []StorageContent{
				{VolID: "local:iso/boot.iso", Content: "iso"},
				{VolID: "local:import/ubuntu.qcow2", Content: "import", Size: 600000000},
			})
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	})

	vol, err := client.FindStorageVolume(context.Background(), "pve01", "local", "local:import/ubuntu.qcow2")
	if err != nil {
		t.Fatalf("FindStorageVolume: %v", err)
	}
	if vol == nil || vol.Size != 600000000 {
		t.Errorf("expected volume with size 600000000, got %v", vol)
	}

	missing, err := client.FindStorageVolume(context.Background(), "pve01", "local", "local:import/missing.qcow2")
	if err != nil {
		t.Fatalf("FindStorageVolume missing: %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing volume, got %v", missing)
	}
}

func TestDownloadURL(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api2/json/nodes/pve01/storage/local/download-url" {
			writeData(t, w, "UPID:pve01:download:test")
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	})

	upid, err := client.DownloadURL(context.Background(), "pve01", "local", "https://example.com/image.img", "image.qcow2", "import")
	if err != nil {
		t.Fatalf("DownloadURL: %v", err)
	}
	if upid != "UPID:pve01:download:test" {
		t.Errorf("upid = %q, want 'UPID:pve01:download:test'", upid)
	}
}

func TestCreateVM(t *testing.T) {
	var gotPayload map[string]any
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api2/json/nodes/pve01/qemu" {
			if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			writeData(t, w, nil)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	})

	err := client.CreateVM(context.Background(), "pve01", CreateVMOptions{
		VMID:   3000,
		Name:   "template-test",
		Memory: 2048,
		SCSI0:  "SSD:0,import-from=local:import/image.qcow2",
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if gotPayload["name"] != "template-test" {
		t.Errorf("payload name = %v, want template-test", gotPayload["name"])
	}
}

func TestResizeDisk(t *testing.T) {
	client, _ := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/api2/json/nodes/pve01/qemu/3000/resize" {
			writeData(t, w, nil)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	})

	if err := client.ResizeDisk(context.Background(), "pve01", 3000, "scsi0", "10G"); err != nil {
		t.Fatalf("ResizeDisk: %v", err)
	}
}
