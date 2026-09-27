# ADR-018 — `core.ansible/v1` via an OpenSSH certificate, not via `tsh`

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Context: once the Teleport agent (`ssh_service`) is installed on a VM (M8), the native sshd must be turned off for "direct SSH refused" (doc 08) to hold — which would break future `core.ansible/v1` calls (direct SSH connection with the service private key) to that VM unless the core itself switches to a path that goes through Teleport (doc 07: "the core switches its own SSH runners to ProxyJump through the bastion"). Initial idea: `internal/runner` learns a `ProxyCommand tsh proxy ssh`, which assumes the `tsh` binary is available in the container that runs Ansible (`willhallonline/ansible`, which does not have it) — meaning either a custom Ansible image or mounting the `tsh` binary from a Teleport image on every call.

Checked by hand in Docker: Teleport's `ssh_service` (the node/agent) speaks **standard SSH** on its own port (3022) — not a proprietary protocol. A classic OpenSSH client (not `tsh`) connects to it directly with a user certificate signed by the Teleport CA (`tctl auth sign --format=openssh`), in the `<key>-cert.pub` format that OpenSSH natively picks up next to the private key. `tsh`/the multiplexed proxy (3080) are only needed to reach a node that is not directly reachable on the network (access from outside through a reverse tunnel) — not relevant here, every VM in the fleet is on the same private network.

Decision: `ansiblev1.Target` (`core.ansible/v1`) gains an optional `ssh_certificate_pem` field (additive change); when set, the certificate is written next to the private key as `<keyfile>-cert.pub` before starting the existing Ansible container, with no image change at all. Consequence: no dependency on `tsh` in the Ansible pipeline, a minimal core change to carry the "SSH Repoint" of doc 07.
