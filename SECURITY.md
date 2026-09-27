# Security policy

Genesis handles secrets, a PKI and SSH access to an entire infrastructure: vulnerabilities are taken seriously.

## Reporting a vulnerability

**Do not open a public issue.** Use GitHub's private reporting: the repository's **Security** tab → **Report a vulnerability** ([direct link](https://github.com/WhiteRoseLK/genesis/security/advisories/new)).

If possible, include:

- the affected version or commit;
- the component (core layer or module);
- a minimal reproduction scenario, **without any real secret**;
- the estimated impact.

Receipt is acknowledged within 7 days. The fix is prepared in a private security advisory, then published with a fixed release, crediting the reporter if they wish.

## Supported versions

While the project is at `0.x`, only the latest release (and `main`) receives security fixes.

## Scope

In scope, among others: secret leaks (logs, state, errors, temporary files), broker bypass (calling a function not declared in `requires`), privilege escalation on the seed, tampering with an installed module (`genesis.lock`), supply chain (images, dependencies).
