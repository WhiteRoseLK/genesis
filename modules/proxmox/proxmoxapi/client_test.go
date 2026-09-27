// SPDX-License-Identifier: Apache-2.0

package proxmoxapi

import (
	"context"
	"encoding/json"
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
