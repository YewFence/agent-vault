---
name: agent-vault-cli
description: >-
  Use to diagnose failed or denied outbound HTTP(S) or WebSocket requests when
  AGENT_VAULT_ACTIVE=true, the user identifies Agent Vault as relevant, or the
  response contains Agent Vault information. Agent Vault
  may inject credentials or replace configured placeholders on the wire. Keep
  using the real upstream URL and ensure the client respects proxy environment
  variables. Do not use this skill for successful outbound requests.
---

# Agent Vault

Agent Vault routes outbound requests through a proxy and attaches configured
credentials while forwarding them to the real upstream service. Call the real
upstream URL and use the service's normal SDK or HTTP authentication interface.
Do not call an Agent Vault URL in place of the upstream URL.

`AGENT_VAULT_ACTIVE=true` means this process was started by `agent-vault vault
run` and its outbound traffic is configured for Agent Vault.

Keep `HTTPS_PROXY` and `HTTP_PROXY` available to child processes and HTTP
clients. Standard clients such as curl, fetch, requests, axios, and most
official SDKs honor these variables automatically.

## Placeholders

For a configured substitution, AgentVault may provide an environment
variable containing an unprivileged placeholder shaped like the credential an
SDK expects. Use an existing value exactly as supplied wherever the upstream
client normally expects its credential. The proxy replaces it only on the
configured request surfaces.

Never generate or guess a placeholder. Never print, log, persist, decode,
extract, or expose credential values or placeholders. Never use
`AGENT_VAULT_TOKEN` as an upstream credential; it authenticates only with Agent
Vault.

## Agent token renewal

When a long-lived `av_agt_` token needs regular replacement, run `agent-vault agent token renew`. It reads `AGENT_VAULT_ADDR` and `AGENT_VAULT_TOKEN` from the runtime environment and prints only the replacement token. For a persistent runtime token file, run `agent-vault agent token renew --token-file <path>`; it replaces the file atomically.

Do not renew the same token from more than one process. If the renewal response is lost or renewal reports that the token is no longer current, the runtime cannot retrieve the replacement. Ask an authorized operator to run `agent-vault agent rotate <name>`.

## Diagnose failures

Inspect the HTTP status and structured response before changing the request.
Check the upstream host, authentication mechanism, proxy environment, and
whether the client honors the proxy. Keep diagnostics free of credential and
placeholder values.

- `401` may mean the Agent Vault session expired or the upstream rejected the
  configured credential. Report which endpoint rejected the request and ask
  the user to refresh the relevant access when local request construction and
  proxy routing are correct.
- `403` with `proposal_hint` means the target is not configured for this vault.
  Explain the missing access to the user. Do not create a proposal automatically.
- `403` with `service_disabled` requires an operator to enable the service.
- `403` with `ssrf_blocked` means Agent Vault's network policy rejected the
  destination before contacting it. Report the blocked destination and ask an
  operator to review the private-range policy or narrow network allowlist; do
  not retry blindly.
- `502` may mean a credential is missing, the upstream is unreachable, or an
  OAuth connection needs attention. Report the structured error code and the
  local checks already performed instead of retrying blindly.

Create, inspect, or poll a proposal only when the user explicitly asks you to
handle it. Before doing so, read
[references/proposals.md](references/proposals.md). When this file was fetched
from the Agent Vault server, fetch the reference at
`/v1/skills/agent-vault-cli/references/proposals.md` from the same origin.
