// SPDX-License-Identifier: Apache-2.0

package modulehost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"genesis/internal/broker"
	"genesis/internal/runner"
	sdk "genesis/sdk/go"
	dnszonev1 "genesis/sdk/go/gen/functions/dns/zone/v1"
	modulev1 "genesis/sdk/go/gen/module/v1"
)

func buildCoreDNS(t *testing.T) (binaryPath string, manifest *sdk.ManifestFile) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceDir := filepath.Join(wd, "..", "..", "modules", "coredns")

	binaryPath = filepath.Join(t.TempDir(), BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compilation de coredns : %v\n%s", err, out)
	}
	manifest, err = sdk.LoadManifest(filepath.Join(sourceDir, "module.yaml"))
	if err != nil {
		t.Fatalf("chargement du manifest : %v", err)
	}
	return binaryPath, manifest
}

// resolveWith interroge le serveur DNS à ip:53 depuis un conteneur tiers
// (nslookup, via bind-tools) : même limite d'environnement que les autres
// tests J5/J6, pas de dial réseau direct vers un conteneur depuis ce process.
func resolveWith(t *testing.T, rt *runner.ContainerRuntime, ip, name string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := rt.Run(ctx, runner.RunOptions{
		Image:   "alpine:3",
		Command: []string{"sh", "-c", "apk add -q --no-cache bind-tools >/dev/null 2>&1 && nslookup " + name + " " + ip},
	})
	if err != nil {
		t.Fatalf("nslookup %s @%s : %v", name, ip, err)
	}
	return result.Stdout + result.Stderr
}

// TestCoreDNSResolvesUpsertedRecord est le critère "Verify du doc07" du
// jalon J6 (doc 08) pour coredns : la zone est réellement interrogeable
// après UpsertRecord, et le retrait après DeleteRecord est réel aussi —
// vérifié par une vraie requête DNS, pas la parole du module.
func TestCoreDNSResolvesUpsertedRecord(t *testing.T) {
	rt, err := runner.DetectContainerRuntime("auto")
	if err != nil {
		t.Skipf("aucun runtime de conteneur disponible : %v", err)
	}
	registry := broker.NewRegistry()
	registry.SetNative("core.container/v1", broker.NativeContainer(rt))

	binaryPath, manifest := buildCoreDNS(t)
	client, err := Launch(binaryPath, manifest)
	if err != nil {
		t.Fatalf("Launch : %v", err)
	}
	defer client.Close()
	ctx := context.Background()

	token := registry.OpenSession(client.Broker(), "coredns", []string{"core.container/v1"})
	checkResp, err := client.Module().Check(ctx, &modulev1.StepRequest{RunId: "test", BrokerToken: token})
	if err != nil {
		t.Fatalf("Check : %v", err)
	}
	if checkResp.GetStatus() != modulev1.CheckResult_STATUS_CONFORME {
		t.Fatalf("Check().Status = %v, attendu CONFORME", checkResp.GetStatus())
	}

	conn, err := client.DispenseFunction("dns.zone/v1")
	if err != nil {
		t.Fatalf("DispenseFunction(dns.zone/v1) : %v", err)
	}
	zoneClient := dnszonev1.NewDnsZoneClient(conn)

	if _, err := zoneClient.UpsertRecord(ctx, &dnszonev1.Record{
		Zone: "lab.internal", Name: "infra01", Type: "A", Values: []string{"10.10.0.5"}, Ttl: 300,
	}); err != nil {
		t.Fatalf("UpsertRecord : %v", err)
	}
	t.Cleanup(func() {
		_, _ = zoneClient.DeleteRecord(context.Background(), &dnszonev1.RecordKey{Zone: "lab.internal", Name: "infra01", Type: "A"})
	})

	endpoint, err := zoneClient.Endpoint(ctx, &dnszonev1.Empty{})
	if err != nil {
		t.Fatalf("Endpoint : %v", err)
	}
	if endpoint.GetAddress() == "" {
		t.Fatal("Endpoint().Address vide")
	}

	records, err := zoneClient.ListRecords(ctx, &dnszonev1.Zone{Zone: "lab.internal"})
	if err != nil {
		t.Fatalf("ListRecords : %v", err)
	}
	if len(records.GetRecords()) != 1 {
		t.Fatalf("ListRecords = %+v, attendu 1 enregistrement", records.GetRecords())
	}

	// Requête DNS réelle, depuis un autre conteneur (docs/08-jalons.md, J6 :
	// "Verify du doc 07").
	out := resolveWith(t, rt, endpoint.GetAddress(), "infra01.lab.internal")
	if !strings.Contains(out, "10.10.0.5") {
		t.Errorf("nslookup infra01.lab.internal n'a pas résolu vers 10.10.0.5 :\n%s", out)
	}

	if _, err := zoneClient.DeleteRecord(ctx, &dnszonev1.RecordKey{Zone: "lab.internal", Name: "infra01", Type: "A"}); err != nil {
		t.Fatalf("DeleteRecord : %v", err)
	}

	out = resolveWith(t, rt, endpoint.GetAddress(), "infra01.lab.internal")
	if strings.Contains(out, "10.10.0.5") {
		t.Errorf("infra01.lab.internal résout encore après DeleteRecord :\n%s", out)
	}
}
