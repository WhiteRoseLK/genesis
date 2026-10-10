# ADR-0028 — `core.ansible/v1` SSH host key verification via `known_hosts`

- **Status**: accepted
- **Discussion**: [#28](https://github.com/WhiteRoseLK/genesis/issues/28)

Context: the `core.ansible/v1` runner generated an inventory that disabled SSH host key verification (`StrictHostKeyChecking=no`, `UserKnownHostsFile=/dev/null`, `ANSIBLE_HOST_KEY_CHECKING=False`). This presented a risk: a network interception between the seed and a target VM (IP spoofing) could intercept secrets, configuration, and certificates passed in extra-vars.

Alternatives:
(a) Globally enforce host key checking without providing a host key mechanism — rejected: would break all Ansible connections since host keys are unknown to the runner container.
(b) Scan host keys dynamically (`ssh-keyscan`) before running Ansible — rejected: vulnerable to interception during the initial scan (trust on first use).
(c) Support an optional `ssh_host_key` in `ansiblev1.Target`, generating `/work/known_hosts` and enabling `StrictHostKeyChecking=yes` when set, while preserving fallback when empty — accepted: secure, backward-compatible, and eliminates MITM risks when host keys are known.

Decision:
- `ansiblev1.Target` gains an optional `ssh_host_key` field (OpenSSH public key).
- When `ssh_host_key` is provided:
  - Generate `/work/known_hosts` in the container's tmpfs with mode `0600`.
  - Format known_hosts according to standard OpenSSH syntax (`host` for port 22/0, `[host]:port` otherwise).
  - Set `ansible_ssh_common_args='-o StrictHostKeyChecking=yes -o UserKnownHostsFile=/work/known_hosts'`.
  - Set `ANSIBLE_HOST_KEY_CHECKING: "True"` in the Ansible container environment.
- When `ssh_host_key` is omitted, retain `StrictHostKeyChecking=no` and `ANSIBLE_HOST_KEY_CHECKING: "False"` for backward compatibility.
- Propagate `ssh_host_public_key` across `computevmv1.EnsureVMRequest` and `computevmv1.VM`, and pass `ssh_host_key` in `osbasev1.Target` and `fleetagentv1.Target`.

Consequences:
- Playbook runs against targets with known host keys are protected against spoofing/MITM interception.
- Existing targets without configured host keys continue to operate without breaking changes.
