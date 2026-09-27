// SPDX-License-Identifier: Apache-2.0

//go:build docker

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/WhiteRoseLK/genesis/internal/broker"
	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	"github.com/WhiteRoseLK/genesis/internal/runner"
	"github.com/WhiteRoseLK/genesis/internal/testutil"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	dnszonev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

func buildCoreDNS(t *testing.T) (binaryPath string, manifest *sdk.ManifestFile) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(wd, "..", "..", "modules", "coredns")

	binaryPath = filepath.Join(t.TempDir(), modulehost.BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compilation de coredns: %v\n%s", err, out)
	}
	manifest, err = sdk.LoadManifest(filepath.Join(sourceDir, "module.yaml"))
	if err != nil {
		t.Fatalf("loading the manifest: %v", err)
	}
	return binaryPath, manifest
}

// resolveWith queries the DNS server at ip:53 from a third-party container
// (busybox's nslookup, included in alpine: no package to download): the same
// environment limit as the other M5/M6 tests, no direct network dial to a
// container from this process.
func resolveWith(t *testing.T, rt *runner.ContainerRuntime, ip, name string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := rt.Run(ctx, runner.RunOptions{
		Image:   "alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6",
		Command: []string{"nslookup", name, ip},
	})
	if err != nil {
		t.Fatalf("nslookup %s @%s: %v", name, ip, err)
	}
	return result.Stdout + result.Stderr
}

// TestCoreDNSResolvesUpsertedRecord is the "Verify of doc 07" criterion of
// milestone M6 (doc 08) for coredns: the zone can really be queried after
// UpsertRecord, and the removal after DeleteRecord is real too — checked with
// a real DNS query, not by taking the module's word for it.
func TestCoreDNSResolvesUpsertedRecord(t *testing.T) {
	rt := testutil.RequireRuntime(t)
	registry := broker.NewRegistry()
	registry.SetNative("core.container/v1", broker.NativeContainer(rt))

	binaryPath, manifest := buildCoreDNS(t)
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(client.Close)
	ctx := context.Background()

	token := registry.OpenSession(client.Broker(), "coredns", []string{"core.container/v1"})
	checkResp, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_TODO {
		t.Fatalf("Check().Status = %v, want TODO (not bootstrapped yet)", checkResp.GetStatus())
	}

	seedToken := registry.OpenSession(client.Broker(), "coredns", []string{"core.container/v1"})
	seedUpResp, err := client.Module().SeedUp(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: seedToken})
	if err != nil {
		t.Fatalf("SeedUp: %v", err)
	}
	if seedUpResp.GetStatus() != modulev1.StepResult_STATUS_OK {
		t.Fatalf("SeedUp().Status = %v", seedUpResp.GetStatus())
	}
	// Stop the CoreDNS container even on failure: otherwise its fixed name
	// blocks the following runs ("container name already in use").
	t.Cleanup(func() {
		downToken := registry.OpenSession(client.Broker(), "coredns", []string{"core.container/v1"})
		_, _ = client.Module().Destroy(context.Background(), &modulev1.StepRequest{RunId: "test", BrokerToken: downToken})
	})

	conn, err := client.DispenseFunction("dns.zone/v1")
	if err != nil {
		t.Fatalf("DispenseFunction(dns.zone/v1): %v", err)
	}
	zoneClient := dnszonev1.NewDnsZoneClient(conn)

	if _, err := zoneClient.UpsertRecord(ctx, &dnszonev1.Record{
		Zone: "lab.internal", Name: "infra01", Type: "A", Values: []string{"10.10.0.5"}, Ttl: 300,
	}); err != nil {
		t.Fatalf("UpsertRecord: %v", err)
	}
	t.Cleanup(func() {
		_, _ = zoneClient.DeleteRecord(context.Background(), &dnszonev1.RecordKey{Zone: "lab.internal", Name: "infra01", Type: "A"})
	})

	endpoint, err := zoneClient.Endpoint(ctx, &dnszonev1.Empty{})
	if err != nil {
		t.Fatalf("Endpoint: %v", err)
	}
	if endpoint.GetAddress() == "" {
		t.Fatal("empty Endpoint().Address")
	}

	records, err := zoneClient.ListRecords(ctx, &dnszonev1.Zone{Zone: "lab.internal"})
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(records.GetRecords()) != 1 {
		t.Fatalf("ListRecords = %+v, want 1 record", records.GetRecords())
	}

	// Real DNS query, from another container (docs/08-milestones.md, M6:
	// "Verify of doc 07").
	out := resolveWith(t, rt, endpoint.GetAddress(), "infra01.lab.internal")
	if !strings.Contains(out, "10.10.0.5") {
		t.Errorf("nslookup infra01.lab.internal did not resolve to 10.10.0.5:\n%s", out)
	}

	if _, err := zoneClient.DeleteRecord(ctx, &dnszonev1.RecordKey{Zone: "lab.internal", Name: "infra01", Type: "A"}); err != nil {
		t.Fatalf("DeleteRecord: %v", err)
	}

	out = resolveWith(t, rt, endpoint.GetAddress(), "infra01.lab.internal")
	if strings.Contains(out, "10.10.0.5") {
		t.Errorf("infra01.lab.internal still resolves after DeleteRecord:\n%s", out)
	}
}
