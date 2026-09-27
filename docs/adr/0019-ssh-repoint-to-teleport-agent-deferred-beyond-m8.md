# ADR-019 — SSH repoint to the Teleport agent deferred beyond M8

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Context: doc 08 required "direct SSH refused" for M8. Turning off the native sshd in `fleet.agent/v1.Install` would break every later `core.ansible/v1` call to the VM (e.g. `vault`'s `Handover`) as long as callers do not use the Teleport certificate on port 3022. The core-side mechanism exists (ADR-018: `ansiblev1.Target.ssh_certificate_pem`, handled by `internal/broker`), but nothing feeds it: a module would have to obtain a user certificate (`access.ssh/v1.SignUserKey`, currently `Unimplemented`) and switch its target (port, certificate) after the agent enrols — work that cuts across every target module. Alternatives: (a) do it all in M8 — rejected by the user, scope too wide for the milestone; (b) turn off sshd without a repoint — rejected, it breaks later calls.

Decision (user): `fleet.agent/v1.Install` installs and enrols the Teleport agent but **leaves the native sshd active**; `core.ansible/v1` keeps connecting with a key on port 22. `Verify(teleport)` proves a real SSH connection through the agent (`tctl auth sign` certificate), not that direct access is refused. M8 criterion adjusted (doc 08), debt tracked by a dedicated issue.

Consequence: M8 delivers a working Teleport across the whole fleet without access hardening; the repoint (feeding `ssh_certificate_pem`, `SignUserKey`, turning off sshd) remains to be done in a later milestone.
