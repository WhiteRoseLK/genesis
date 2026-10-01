# Changelog

## [0.2.1](https://github.com/WhiteRoseLK/genesis/compare/v0.2.0...v0.2.1) (2026-10-01)


### Features

* **cli:** implement validate, status and destroy commands ([#77](https://github.com/WhiteRoseLK/genesis/issues/77)) ([6d92130](https://github.com/WhiteRoseLK/genesis/commit/6d921301023816683ee1e43d6d09ccec509bdaad))
* **dns:** persist connection parameters in coredns and powerdns step state and redial broker ([#82](https://github.com/WhiteRoseLK/genesis/issues/82)) ([8418480](https://github.com/WhiteRoseLK/genesis/commit/8418480d1905d7d1f1219b668c474a512754d8a8)), closes [#10](https://github.com/WhiteRoseLK/genesis/issues/10)
* **modules:** chrony/powerdns/vault remember Teleport enrolment and switch their own core.ansible/v1 calls ([#78](https://github.com/WhiteRoseLK/genesis/issues/78)) ([f746f47](https://github.com/WhiteRoseLK/genesis/commit/f746f4731b3dccba6c46ce5b11cbdd7b87eec862))
* **modules:** teleport implements access.ssh/v1.SignUserKey ([#69](https://github.com/WhiteRoseLK/genesis/issues/69)) ([8d5885e](https://github.com/WhiteRoseLK/genesis/commit/8d5885e4d3cdf10dc980ce4f6391ffad32655082))
* **modules:** teleport turns off sshd once enrolment is confirmed, Verify restores "direct SSH refused" ([#79](https://github.com/WhiteRoseLK/genesis/issues/79)) ([232b372](https://github.com/WhiteRoseLK/genesis/commit/232b372476f2bd640fafdd74e22439cf5de287f1)), closes [#16](https://github.com/WhiteRoseLK/genesis/issues/16)
* **proxmox:** add tls_insecure and custom CA support in schema and REST client ([#74](https://github.com/WhiteRoseLK/genesis/issues/74)) ([56a2a12](https://github.com/WhiteRoseLK/genesis/commit/56a2a127994b407d24c0dd888fa58e41b2b4337b))
* **secrets:** switch file store to read-only after migration and route core.secrets/v1 ([#80](https://github.com/WhiteRoseLK/genesis/issues/80)) ([dc31c32](https://github.com/WhiteRoseLK/genesis/commit/dc31c326263fac031b98e0b19a377de7ddaee23e)), closes [#27](https://github.com/WhiteRoseLK/genesis/issues/27)
* **teleport:** persist ownTarget and caPin in step state and redial broker on each call ([#81](https://github.com/WhiteRoseLK/genesis/issues/81)) ([2643ff5](https://github.com/WhiteRoseLK/genesis/commit/2643ff5462139033039335ad6d2870e9855e83b2)), closes [#26](https://github.com/WhiteRoseLK/genesis/issues/26)
* **vault:** implement secrets.kv/v1.List ([#76](https://github.com/WhiteRoseLK/genesis/issues/76)) ([f1f1c31](https://github.com/WhiteRoseLK/genesis/commit/f1f1c319b6de31b5d4deca63255c124a19b8373e))


### Bug Fixes

* **broker:** write the SSH certificate next to the private key when supplied ([#68](https://github.com/WhiteRoseLK/genesis/issues/68)) ([2409438](https://github.com/WhiteRoseLK/genesis/commit/2409438de300e42a9b9df3e4aaa450f673bf4aec))
* **modules:** Check inspects state rather than unconditionally returning COMPLIANT ([#75](https://github.com/WhiteRoseLK/genesis/issues/75)) ([0a869b9](https://github.com/WhiteRoseLK/genesis/commit/0a869b96f70c9465a82bc6589538efdabd7159ea))


### Refactoring

* switch the core code to English ([#56](https://github.com/WhiteRoseLK/genesis/issues/56)) ([53973eb](https://github.com/WhiteRoseLK/genesis/commit/53973eb1b42f954a4cb0f119bde8590b625b8f64))
* switch the modules and tests to English ([#57](https://github.com/WhiteRoseLK/genesis/issues/57)) ([fcd5607](https://github.com/WhiteRoseLK/genesis/commit/fcd560771fb1a957849a7f7f97877cd3d7112d2d))


### Documentation

* add a code of conduct (Contributor Covenant 2.1) ([#53](https://github.com/WhiteRoseLK/genesis/issues/53)) ([ead8d40](https://github.com/WhiteRoseLK/genesis/commit/ead8d405ce33e9dc3bb92832a3c94a487d3710a2))
* jalons suivis sur GitHub, ADR en fichiers numérotés par issue ([#50](https://github.com/WhiteRoseLK/genesis/issues/50)) ([8807d60](https://github.com/WhiteRoseLK/genesis/commit/8807d60455dc6db61b0a4f14b05435a3d5d41a45)), closes [#49](https://github.com/WhiteRoseLK/genesis/issues/49)
* README plus accueillant (bandeau, schémas, exemple, feuille de route) ([#42](https://github.com/WhiteRoseLK/genesis/issues/42)) ([9f59a50](https://github.com/WhiteRoseLK/genesis/commit/9f59a503f233a0d38fe55393e2b2424f2130951e))
* switch the documentation and repository metadata to English ([#55](https://github.com/WhiteRoseLK/genesis/issues/55)) ([c8dad54](https://github.com/WhiteRoseLK/genesis/commit/c8dad5467f7839a95b0724c8cdc28fe2f6193f0e)), closes [#54](https://github.com/WhiteRoseLK/genesis/issues/54)

## [0.2.0](https://github.com/WhiteRoseLK/genesis/compare/v0.1.0...v0.2.0) (2026-09-27)


### ⚠ BREAKING CHANGES

* chemins d'import du SDK modifiés (genesis/sdk/... -> github.com/WhiteRoseLK/genesis/sdk/...).

### Fonctionnalités

* add VM.ssh_port and RunResponse.ip (J6 prep) ([78612fc](https://github.com/WhiteRoseLK/genesis/commit/78612fce231c9af8e584eb3b17e35d1c67f8405f))
* chrony module (time.ntp/v1) for J6 ([2f7bf5a](https://github.com/WhiteRoseLK/genesis/commit/2f7bf5a1ff2153bf46a10e7dbbdcdd9ed26d1013))
* chrony/powerdns/vault appellent fleet.agent/v1 depuis Configure (J8) ([e0882a7](https://github.com/WhiteRoseLK/genesis/commit/e0882a7c24c2770972593ef19e67b28e19568623))
* core.container/v1, core.ansible/v1, os.base/v1 protocols + runner (J5) ([608b83f](https://github.com/WhiteRoseLK/genesis/commit/608b83f9b77227bce4625ba7b7d9eb5ad770d1b1))
* coredns module (dns.zone/v1, dns.resolver/v1) for J6 ([dbf00d9](https://github.com/WhiteRoseLK/genesis/commit/dbf00d968b54ce9fd358f04d63dec2afe8467ad0))
* cross-module Handover/Repoint cycle in internal/engine (J6) ([c927474](https://github.com/WhiteRoseLK/genesis/commit/c92747419ace75d3e0c819994176ef19ad1aa2fb))
* function protocols and bidirectional broker plumbing (J4) ([8dfc145](https://github.com/WhiteRoseLK/genesis/commit/8dfc145ceb4d94697222a808225609a67a77c2b3))
* general spec loading and structural validation (J1) ([0268d17](https://github.com/WhiteRoseLK/genesis/commit/0268d175d2eac4043cddeb24908d6863a57b7934))
* genesis modules scaffold + CLI wiring, Makefile auto-discovery (J3) ([e44aa40](https://github.com/WhiteRoseLK/genesis/commit/e44aa40dc2763d868ba2046b0c9dedea0d3e0c3d))
* internal/broker — function routing and access control (J4) ([99184e9](https://github.com/WhiteRoseLK/genesis/commit/99184e9e36a7617f75d86c98813047c181790c34))
* internal/engine — execute the plan end to end (J4) ([12d3f4d](https://github.com/WhiteRoseLK/genesis/commit/12d3f4dfd808f7d9dc70e0be4c8e7e4754d0ce04))
* internal/planner — module DAG, cycle detection (J4) ([2b4e2fe](https://github.com/WhiteRoseLK/genesis/commit/2b4e2fe2ad6c19f03b20c71e5188199fc9890ff2))
* internal/resolver — capability to module resolution (J4) ([63700c5](https://github.com/WhiteRoseLK/genesis/commit/63700c521cdb1b9fac308e4929b2eb43a44ce65d))
* mecanisme fonctions "de parc" (fleet) + certificat SSH ansible (J8) ([b4c53bb](https://github.com/WhiteRoseLK/genesis/commit/b4c53bb6fe54c4093875c669050b72105cc7903e))
* migration file-&gt;vault dans internal/engine (prep vault, J7) ([e93d781](https://github.com/WhiteRoseLK/genesis/commit/e93d781231178d2f3afa17fc0520e6a8f359940d))
* module host (discovery, launch, supervision) and genesis.lock (J3) ([74d7abc](https://github.com/WhiteRoseLK/genesis/commit/74d7abc202296f8b0d4e8286790aeeb8929ac67d))
* module teleport (access.ssh/v1, fleet.agent/v1) pour J8 ([c4cd86f](https://github.com/WhiteRoseLK/genesis/commit/c4cd86f3134fe07b8388cf58ee22862b27d6f524))
* modules/base-os — TrustCA/SetResolver/SetNTP, Harden stubbed (J5) ([8e09ad1](https://github.com/WhiteRoseLK/genesis/commit/8e09ad1decaecdce7568dbba18b3705140f998f6))
* modules/fake-compute — compute.vm/v1 in-memory provider (J4) ([d6a582a](https://github.com/WhiteRoseLK/genesis/commit/d6a582a7b223e2a6a55760d1b6d4207a99aca90a))
* modules/proxmox — compute.vm/v1 on a real Proxmox VE cluster (J5) ([77f483c](https://github.com/WhiteRoseLK/genesis/commit/77f483c1f5e1b563a4363de49480095163990105))
* native core.container/v1 and core.ansible/v1 (J5) ([3700466](https://github.com/WhiteRoseLK/genesis/commit/370046608602cd3389b4b70ecd8f05f281298702))
* powerdns module (dns.zone/v1, dns.resolver/v1 cible) for J6 ([262564d](https://github.com/WhiteRoseLK/genesis/commit/262564d829f4f423900add9a07a8f6049e8832df))
* proxmoxapi — hand-rolled Proxmox VE REST client (J5) ([618d170](https://github.com/WhiteRoseLK/genesis/commit/618d1702fa875901f09cf9c26521758c44ff8245))
* retrait automatique de la graine en fin d'apply (J8) ([6ab36f3](https://github.com/WhiteRoseLK/genesis/commit/6ab36f3606754f866d3933f98766fa421c0b3b0e))
* secret generation, redaction, and file backend (J2) ([1ddb12a](https://github.com/WhiteRoseLK/genesis/commit/1ddb12a2dce62d11c3c3df2933d2bf7b0ff480da))
* state store with atomic writes and a concurrency lock (J2) ([7a9fb9a](https://github.com/WhiteRoseLK/genesis/commit/7a9fb9a3b400b6f5b553da399a083a8890428a31))
* step-ca module (pki.issuer/v1 graine) pour J7 ([22b5b1b](https://github.com/WhiteRoseLK/genesis/commit/22b5b1b940ba93937da99d16088dce03516e9220))
* upgrade fake-compute to real Docker-backed VMs (J6 prep) ([ca9def9](https://github.com/WhiteRoseLK/genesis/commit/ca9def971a89fc3e12407115a0435d7f60ebc9ca))
* vault module (pki.issuer/v1, secrets.kv/v1 cible) pour J7 ([5759997](https://github.com/WhiteRoseLK/genesis/commit/575999775f67623281aa0f56c01f61407dbc1206))
* wire genesis init and secrets list/get to real backends (J2) ([7162132](https://github.com/WhiteRoseLK/genesis/commit/71621327c72ce09a072cf6e5598d7eb89339d7b0))
* wire genesis plan/apply to resolver+planner+engine (J4) ([4fe4f57](https://github.com/WhiteRoseLK/genesis/commit/4fe4f577559fc5959bc77370b4e1bce2ad4ca2b1))


### Corrections

* **broker:** secrets Ansible hors du disque de la graine ([#19](https://github.com/WhiteRoseLK/genesis/issues/19)) ([4f51d4e](https://github.com/WhiteRoseLK/genesis/commit/4f51d4e2382e94e56ab81ff831f497852d14afff))
* contrainte core des modules compatible avec la release 0.2.0 ([#40](https://github.com/WhiteRoseLK/genesis/issues/40)) ([b78e110](https://github.com/WhiteRoseLK/genesis/commit/b78e110e6464a04bc58e11eeb897b35ed5ba3e10))
* make resolver.FunctionProviders phase-aware (seed vs target) ([8c7618e](https://github.com/WhiteRoseLK/genesis/commit/8c7618ee193192e2a817ed4c01a56566a8769012))
* **proxmox:** renseigner ssh_port (22) dans compute.vm/v1 ([#25](https://github.com/WhiteRoseLK/genesis/issues/25)) ([718b2a6](https://github.com/WhiteRoseLK/genesis/commit/718b2a67b80fe9a8e1c92315f67ebec276baa08a))
* step-ca peut signer un intermediaire tiers (prep vault, J7) ([8765a28](https://github.com/WhiteRoseLK/genesis/commit/8765a28c551e48683eb70f26517fd67bb81e92c9))
* **step-ca:** clé de la CA racine hors du disque de la graine ([#23](https://github.com/WhiteRoseLK/genesis/issues/23)) ([9d8c3f0](https://github.com/WhiteRoseLK/genesis/commit/9d8c3f04f2a8d424666b7c5e7c8ce34e1b962b34))
* wire real capability config and resolve *_ref into StepRequest (J5) ([616dee4](https://github.com/WhiteRoseLK/genesis/commit/616dee4378c0b0237f14a0ff65706581480f9eb8))


### Refactorisation

* extract internal/atomicfile from secrets/state duplicates ([52b8cc8](https://github.com/WhiteRoseLK/genesis/commit/52b8cc8c1469814eaad7d4e0ef3c8c99d230c1fe))


### Documentation

* add ADR-015 for the JSON Schema validation dependency ([8bd3e5a](https://github.com/WhiteRoseLK/genesis/commit/8bd3e5acddd58a6d8d5f893dadc52a2d54b8a20d))
* add Genesis design documentation ([992e240](https://github.com/WhiteRoseLK/genesis/commit/992e2406889a0b0987d9aad2a3dc5e4172209d61))
* ADR-017, teleport remplace le bastion OpenSSH classique (J8) ([0ab7a3e](https://github.com/WhiteRoseLK/genesis/commit/0ab7a3ef0638990873d62abf8b88d0791d3e90e5))
* ADR-019 repoint SSH différé, critères J8 et PROGRESS à jour ([13031b7](https://github.com/WhiteRoseLK/genesis/commit/13031b7d3b805768b37172c89f73be72a50ceea2))
* ADR-020 retrait automatique de la graine, J8 terminé ([7c4e263](https://github.com/WhiteRoseLK/genesis/commit/7c4e263ca3f4bbe2497f11d001fc3e189584f179))
* mark J0 done in PROGRESS.md ([6e9330f](https://github.com/WhiteRoseLK/genesis/commit/6e9330f2c497b2002fa8dc65f13357e8a3638816))
* mark J1 done in PROGRESS.md ([3191566](https://github.com/WhiteRoseLK/genesis/commit/3191566f102ba02a8b3a5d21e463d6ae68fd376e))
* mark J2 done in PROGRESS.md ([f4bab91](https://github.com/WhiteRoseLK/genesis/commit/f4bab915e0277fb4f3f0c4e4b48b2c91a7744b24))
* mark J3 done in PROGRESS.md ([3d13d01](https://github.com/WhiteRoseLK/genesis/commit/3d13d01a790439f74848ffd6808c0ddf215aa1ef))
* mark J4 done in PROGRESS.md ([3b02180](https://github.com/WhiteRoseLK/genesis/commit/3b02180f3f499272a723a53770b7291abf787331))
* mark J5 done (partial, Harden deferred) in PROGRESS.md ([6d97758](https://github.com/WhiteRoseLK/genesis/commit/6d97758f6d3ab6cc67431fde4f23ef95cfa2fc23))
* mark J6 done in PROGRESS.md ([a89047f](https://github.com/WhiteRoseLK/genesis/commit/a89047fae7c143c7355db67a9fa13a14b2bc72c9))
* mark J7 done in PROGRESS.md ([0cfea62](https://github.com/WhiteRoseLK/genesis/commit/0cfea62d353a105393fb413befbf39985a46942a))
* PROGRESS allégé, journal séparé, README, SECURITY, CODEOWNERS ([#31](https://github.com/WhiteRoseLK/genesis/issues/31)) ([d9adf28](https://github.com/WhiteRoseLK/genesis/commit/d9adf28b723075406b26bd69651329a9f3603906))


### Build

* chemins de module github.com/WhiteRoseLK/genesis, go.mod autonomes ([#20](https://github.com/WhiteRoseLK/genesis/issues/20)) ([c99bee3](https://github.com/WhiteRoseLK/genesis/commit/c99bee3c310b86f8d733ea8b06b5f90e38726cab))
