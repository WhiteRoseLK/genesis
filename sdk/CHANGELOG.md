# Changelog

## [0.2.1](https://github.com/WhiteRoseLK/genesis/compare/sdk/v0.2.0...sdk/v0.2.1) (2026-10-11)


### Features

* **ansible:** verify SSH host key via known_hosts in core.ansible/v1 ([#89](https://github.com/WhiteRoseLK/genesis/issues/89)) ([fc0e21a](https://github.com/WhiteRoseLK/genesis/commit/fc0e21aceb937673e457f4ee61dda76476b17e30))
* **dns:** automatic reverse zone and ptr derivation in dns.zone/v1 ([#86](https://github.com/WhiteRoseLK/genesis/issues/86)) ([6d0b537](https://github.com/WhiteRoseLK/genesis/commit/6d0b537b1c1a52b620b3f7a2f6d77980ffa4dba0))
* **engine:** trigger explicit Repoint on handover and update consumer session caching ([#83](https://github.com/WhiteRoseLK/genesis/issues/83)) ([015b4a1](https://github.com/WhiteRoseLK/genesis/commit/015b4a114f8e88d627594455b87f9778b4140d40))
* **modules:** teleport implements access.ssh/v1.SignUserKey ([#69](https://github.com/WhiteRoseLK/genesis/issues/69)) ([8d5885e](https://github.com/WhiteRoseLK/genesis/commit/8d5885e4d3cdf10dc980ce4f6391ffad32655082))
* **resolver:** base module compatibility on protocol version (ADR-0039) ([#90](https://github.com/WhiteRoseLK/genesis/issues/90)) ([290c632](https://github.com/WhiteRoseLK/genesis/commit/290c63267268f599e7e9ed103996b65f40278a49)), closes [#39](https://github.com/WhiteRoseLK/genesis/issues/39)


### Refactoring

* switch the core code to English ([#56](https://github.com/WhiteRoseLK/genesis/issues/56)) ([53973eb](https://github.com/WhiteRoseLK/genesis/commit/53973eb1b42f954a4cb0f119bde8590b625b8f64))


### Documentation

* switch the documentation and repository metadata to English ([#55](https://github.com/WhiteRoseLK/genesis/issues/55)) ([c8dad54](https://github.com/WhiteRoseLK/genesis/commit/c8dad5467f7839a95b0724c8cdc28fe2f6193f0e)), closes [#54](https://github.com/WhiteRoseLK/genesis/issues/54)

## [0.2.0](https://github.com/WhiteRoseLK/genesis/compare/sdk/v0.1.0...sdk/v0.2.0) (2026-09-27)


### ⚠ BREAKING CHANGES

* chemins d'import du SDK modifiés (genesis/sdk/... -> github.com/WhiteRoseLK/genesis/sdk/...).

### Fonctionnalités

* add cloud-init fields to compute.vm/v1 EnsureVMRequest (J5) ([4446119](https://github.com/WhiteRoseLK/genesis/commit/44461196293ee5c3d5196f3b22b240c113f57f3c))
* add VM.ssh_port and RunResponse.ip (J6 prep) ([78612fc](https://github.com/WhiteRoseLK/genesis/commit/78612fce231c9af8e584eb3b17e35d1c67f8405f))
* core.container/v1, core.ansible/v1, os.base/v1 protocols + runner (J5) ([608b83f](https://github.com/WhiteRoseLK/genesis/commit/608b83f9b77227bce4625ba7b7d9eb5ad770d1b1))
* function protocols and bidirectional broker plumbing (J4) ([8dfc145](https://github.com/WhiteRoseLK/genesis/commit/8dfc145ceb4d94697222a808225609a67a77c2b3))
* internal/engine — execute the plan end to end (J4) ([12d3f4d](https://github.com/WhiteRoseLK/genesis/commit/12d3f4dfd808f7d9dc70e0be4c8e7e4754d0ce04))
* mecanisme fonctions "de parc" (fleet) + certificat SSH ansible (J8) ([b4c53bb](https://github.com/WhiteRoseLK/genesis/commit/b4c53bb6fe54c4093875c669050b72105cc7903e))
* migration file-&gt;vault dans internal/engine (prep vault, J7) ([e93d781](https://github.com/WhiteRoseLK/genesis/commit/e93d781231178d2f3afa17fc0520e6a8f359940d))
* module teleport (access.ssh/v1, fleet.agent/v1) pour J8 ([c4cd86f](https://github.com/WhiteRoseLK/genesis/commit/c4cd86f3134fe07b8388cf58ee22862b27d6f524))
* module/v1 protocol definition and generated code (J3) ([5b8e40e](https://github.com/WhiteRoseLK/genesis/commit/5b8e40e70c7109a7d716c2896faa011d5fa1db04))
* sdk.Serve, module.yaml parsing, and conformance harness skeleton (J3) ([0759e47](https://github.com/WhiteRoseLK/genesis/commit/0759e47bae59126fb37fcee8639f4759af64226c))
* step-ca module (pki.issuer/v1 graine) pour J7 ([22b5b1b](https://github.com/WhiteRoseLK/genesis/commit/22b5b1b940ba93937da99d16088dce03516e9220))
* time.ntp/v1, dns.zone/v1, dns.resolver/v1 protocols (J6) ([e0d7a4f](https://github.com/WhiteRoseLK/genesis/commit/e0d7a4f51648144be7290b5486746cdd77137b36))


### Corrections

* step-ca peut signer un intermediaire tiers (prep vault, J7) ([8765a28](https://github.com/WhiteRoseLK/genesis/commit/8765a28c551e48683eb70f26517fd67bb81e92c9))
* **step-ca:** clé de la CA racine hors du disque de la graine ([#23](https://github.com/WhiteRoseLK/genesis/issues/23)) ([9d8c3f0](https://github.com/WhiteRoseLK/genesis/commit/9d8c3f04f2a8d424666b7c5e7c8ce34e1b962b34))


### Build

* chemins de module github.com/WhiteRoseLK/genesis, go.mod autonomes ([#20](https://github.com/WhiteRoseLK/genesis/issues/20)) ([c99bee3](https://github.com/WhiteRoseLK/genesis/commit/c99bee3c310b86f8d733ea8b06b5f90e38726cab))
